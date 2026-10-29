/**
 * \file net-game.c
 * \brief The net front end's structured channel and headless mode
 *
 * Copyright (c) 2026 Fancyband contributors
 *
 * This work is free software; you can redistribute it and/or modify it
 * under the terms of either:
 *
 * a) the GNU General Public License as published by the Free Software
 *    Foundation, version 2, or
 *
 * b) the "Angband licence":
 *    This software may be copied and distributed for educational, research,
 *    and not for profit purposes provided that this copyright and statement
 *    are included in all such copies.  Other copyrights may also apply.
 *
 * Beside the streamed terms, the client can ask for structured data:
 *
 * - EVENT frames report game events, such as a change in hit points.
 * - SNAPSHOT frames describe what the player knows: the character, the
 *   visible monsters, the gear, and the map as seen.
 * - COMMAND frames push game commands onto the command queue.
 * - REQUEST and REPLY frames carry the game's prompts, such as "which
 *   item?", for each kind of prompt the client claims, and hand whole
 *   screens, such as the level map, to the client for each screen it
 *   claims.
 *
 * In headless mode there is no text UI at all: every prompt is a request
 * and only commands drive the game.  proto/README.md describes the JSON.
 */
#include "angband.h"
#include "cave.h"
#include "cmd-core.h"
#include "game-event.h"
#include "game-input.h"
#include "game-world.h"
#include "generate.h"
#include "init.h"
#include "main.h"
#include "mon-predicate.h"
#include "mon-util.h"
#include "monster.h"
#include "net-frontend.h"
#include "net-json.h"
#include "obj-curse.h"
#include "obj-desc.h"
#include "obj-gear.h"
#include "obj-knowledge.h"
#include "obj-pile.h"
#include "obj-util.h"
#include "player.h"
#include "player-birth.h"
#include "grafmode.h"
#include "option.h"
#include "player-calcs.h"
#include "player-spell.h"
#include "player-timed.h"
#include "player-util.h"
#include "savefile.h"
#include "store.h"
#include "trap.h"
#include "ui-command.h"
#include "ui-death.h"
#include "ui-display.h"
#include "ui-event.h"
#include "ui-game.h"
#include "ui-help.h"
#include "ui-input.h"
#include "ui-knowledge.h"
#include "ui-map.h"
#include "ui-object.h"
#include "ui-output.h"
#include "ui-player.h"
#include "ui-prefs.h"
#include "ui-target.h"
#include "ui-term.h"
#ifdef ALLOW_BORG
#include "borg/borg.h"
#endif

#ifdef USE_NET

/**
 * ------------------------------------------------------------------------
 * Capabilities
 * ------------------------------------------------------------------------ */

/**
 * The kinds of prompt a client can claim, one per input hook.
 */
enum net_prompt {
	PROMPT_STRING,
	PROMPT_QUANTITY,
	PROMPT_CHECK,
	PROMPT_CHAR,
	PROMPT_COM,
	PROMPT_DIRECTION,
	PROMPT_AIM,
	PROMPT_GRID,
	PROMPT_SPELL_FROM_BOOK,
	PROMPT_SPELL,
	PROMPT_ITEM,
	PROMPT_CURSE,
	PROMPT_EFFECT,
	PROMPT_CONFIRM_DEBUG,
	PROMPT_MAX
};

static const char *prompt_names[PROMPT_MAX] = {
	"string", "quantity", "check", "char", "com", "direction", "aim",
	"grid", "spell_from_book", "spell", "item", "curse", "effect",
	"confirm_debug"
};

/**
 * The screens a client can draw itself.  Store and birth are reserved for
 * a later version; claiming them changes nothing yet.
 */
enum net_screen {
	SCREEN_OPTIONS,
	SCREEN_INVENTORY,
	SCREEN_LOOK,
	SCREEN_TARGET,
	SCREEN_STORE,
	SCREEN_BIRTH,
	SCREEN_DEATH,
	SCREEN_KNOWLEDGE,
	SCREEN_CHARACTER,
	SCREEN_MESSAGES,
	SCREEN_HELP,
	SCREEN_MAP,
	SCREEN_MAX
};

static const char *screen_names[SCREEN_MAX] = {
	"options", "inventory", "look", "target", "store", "birth", "death",
	"knowledge", "character", "messages", "help", "map"
};

/**
 * The parts of a snapshot, which a client can ask for one by one.
 */
enum {
	PART_PLAYER = 0x01,
	PART_MONSTERS = 0x02,
	PART_GEAR = 0x04,
	PART_FLOOR = 0x08,
	PART_MAP = 0x10,
	PART_STATUS = 0x20,
	PART_ALL = 0x3F
};

static const struct {
	const char *name;
	unsigned bit;
} part_names[] = {
	{ "player", PART_PLAYER },
	{ "monsters", PART_MONSTERS },
	{ "gear", PART_GEAR },
	{ "floor", PART_FLOOR },
	{ "map", PART_MAP },
	{ "status", PART_STATUS }
};

static bool cap_events = false;
static bool cap_snapshots = false;
static bool cap_waits = false;
static bool cap_status = false;
static bool cap_moves = false;
static bool cap_scrolls = false;
static unsigned snapshot_parts = PART_ALL;
static bool claimed[PROMPT_MAX];
static bool screen_claimed[SCREEN_MAX];

/**
 * Turn part names into bits: "player+map" for a capability, or one name
 * at a time.  Unknown names are ignored.
 */
static unsigned parse_parts(const char *names)
{
	unsigned parts = 0;
	size_t i;

	while (*names) {
		size_t len = strcspn(names, "+");

		for (i = 0; i < N_ELEMENTS(part_names); i++) {
			if (strlen(part_names[i].name) == len
					&& !strncmp(names, part_names[i].name, len)) {
				parts |= part_names[i].bit;
			}
		}
		names += len;
		if (*names == '+') names++;
	}
	return parts;
}

/**
 * Read the client's capabilities from its HELLO.
 */
void net_game_hello(const struct net_hello *hello)
{
	int i, j;

	for (i = 0; i < hello->n_caps; i++) {
		const char *cap = hello->caps[i];

		if (streq(cap, "events")) {
			cap_events = true;
		} else if (streq(cap, "snapshots")) {
			cap_snapshots = true;
			snapshot_parts = PART_ALL;
		} else if (prefix(cap, "snapshots:")) {
			cap_snapshots = true;
			snapshot_parts = parse_parts(cap + 10);
		} else if (streq(cap, "waits")) {
			cap_waits = true;
		} else if (streq(cap, "status")) {
			cap_status = true;
		} else if (streq(cap, "moves")) {
			cap_moves = true;
		} else if (streq(cap, "scrolls")) {
			cap_scrolls = true;
		} else if (streq(cap, "screens")) {
			for (j = 0; j < SCREEN_MAX; j++) screen_claimed[j] = true;
		} else if (prefix(cap, "screen:")) {
			for (j = 0; j < SCREEN_MAX; j++) {
				if (streq(cap + 7, screen_names[j])) screen_claimed[j] = true;
			}
		} else if (streq(cap, "prompts")) {
			for (j = 0; j < PROMPT_MAX; j++) claimed[j] = true;
		} else if (prefix(cap, "prompt:")) {
			for (j = 0; j < PROMPT_MAX; j++) {
				if (streq(cap + 7, prompt_names[j])) claimed[j] = true;
			}
		}
	}
}

/**
 * ------------------------------------------------------------------------
 * Sending JSON
 * ------------------------------------------------------------------------ */

/* The frame being written, between net_json_begin() and net_json_end() */
static size_t json_frame_start;

static void net_json_begin(struct net_jw *w, uint8_t type)
{
	json_frame_start = net_frame_begin(&net_out, type);
	net_jw_init(w, &net_out);
	net_jw_obj(w);
}

static void net_json_end(struct net_jw *w)
{
	net_jw_obj_end(w);
	net_frame_end(&net_out, json_frame_start);
}

/**
 * ------------------------------------------------------------------------
 * Context
 * ------------------------------------------------------------------------ */

static cmd_context net_ctx = CTX_INIT;

static const char *context_name(cmd_context ctx)
{
	switch (ctx) {
		case CTX_INIT: return "init";
		case CTX_BIRTH: return "birth";
		case CTX_GAME: return "game";
		case CTX_STORE: return "store";
		case CTX_DEATH: return "death";
	}
	return "unknown";
}

/**
 * ------------------------------------------------------------------------
 * Object references
 *
 * Objects are named by where they are: {"inven":0} is the first pack
 * slot, {"equip":n} an equipment slot, {"quiver":n} a quiver slot,
 * {"floor":n} the nth object under the player, and {"store":n} the nth
 * item for sale in the store the player is in.
 * ------------------------------------------------------------------------ */

static int net_floor_list(struct object **list, int max)
{
	return scan_floor(list, max, player, OFLOOR_SENSE | OFLOOR_VISIBLE,
		NULL);
}

static struct object *net_resolve_item(const struct net_json *ref)
{
	int i;

	if (net_json_get_int(ref, "inven", &i)) {
		return (i >= 0 && i < z_info->pack_size) ?
			player->upkeep->inven[i] : NULL;
	}
	if (net_json_get_int(ref, "equip", &i)) {
		return (i >= 0 && i < player->body.count) ?
			slot_object(player, i) : NULL;
	}
	if (net_json_get_int(ref, "quiver", &i)) {
		return (i >= 0 && i < z_info->quiver_size) ?
			player->upkeep->quiver[i] : NULL;
	}
	if (net_json_get_int(ref, "floor", &i)) {
		struct object **list = mem_zalloc(z_info->floor_size
			* sizeof(*list));
		int n = net_floor_list(list, z_info->floor_size);
		struct object *obj = (i >= 0 && i < n) ? list[i] : NULL;

		mem_free(list);
		return obj;
	}
	if (net_json_get_int(ref, "store", &i)) {
		struct store *store = store_at(cave, player->grid);
		struct object **list;
		struct object *obj;

		if (!store || i < 0 || i >= store->stock_num
				|| i >= z_info->store_inven_max) {
			return NULL;
		}
		list = mem_zalloc(z_info->store_inven_max * sizeof(*list));
		store_stock_list(store, list, z_info->store_inven_max);
		obj = list[i];
		mem_free(list);
		return obj;
	}
	return NULL;
}

static void net_write_ref(struct net_jw *w, const char *where, int index)
{
	net_jw_key(w, "ref");
	net_jw_obj(w);
	net_jw_kint(w, where, index);
	net_jw_obj_end(w);
}

static void net_write_object(struct net_jw *w, const struct object *obj,
		const char *where, int index)
{
	char name[160];

	object_desc(name, sizeof(name), obj, ODESC_PREFIX | ODESC_FULL, player);
	net_jw_obj(w);
	net_write_ref(w, where, index);
	net_jw_kstr(w, "name", name);
	net_jw_kint(w, "number", obj->number);
	net_jw_kint(w, "weight", obj->weight);
	net_jw_obj_end(w);
}

/**
 * ------------------------------------------------------------------------
 * Snapshots
 * ------------------------------------------------------------------------ */

static const char *stat_keys[STAT_MAX] = { "str", "int", "wis", "dex", "con" };

static void net_write_player(struct net_jw *w)
{
	int i;

	net_jw_key(w, "player");
	net_jw_obj(w);
	net_jw_kstr(w, "name", player->full_name);
	net_jw_kstr(w, "race", player->race ? player->race->name : NULL);
	net_jw_kstr(w, "class", player->class ? player->class->name : NULL);
	net_jw_kint(w, "level", player->lev);
	net_jw_kint(w, "exp", player->exp);
	net_jw_kint(w, "max_exp", player->max_exp);
	net_jw_kint(w, "hp", player->chp);
	net_jw_kint(w, "mhp", player->mhp);
	net_jw_kint(w, "sp", player->csp);
	net_jw_kint(w, "msp", player->msp);
	net_jw_kint(w, "gold", player->au);
	net_jw_kint(w, "depth", player->depth);
	net_jw_kint(w, "max_depth", player->max_depth);
	net_jw_kint(w, "x", player->grid.x);
	net_jw_kint(w, "y", player->grid.y);
	net_jw_kint(w, "speed", player->state.speed - 110);
	net_jw_kint(w, "ac", player->known_state.ac + player->known_state.to_a);
	net_jw_kint(w, "food", player->timed[TMD_FOOD]);
	net_jw_kbool(w, "dead", player->is_dead);
	net_jw_key(w, "stats");
	net_jw_obj(w);
	for (i = 0; i < STAT_MAX; i++) {
		net_jw_kint(w, stat_keys[i], player->state.stat_use[i]);
	}
	net_jw_obj_end(w);
	net_jw_obj_end(w);
}

static void net_write_monsters(struct net_jw *w)
{
	int i;

	net_jw_key(w, "monsters");
	net_jw_arr(w);
	if (cave) {
		for (i = 1; i < cave_monster_max(cave); i++) {
			struct monster *mon = cave_monster(cave, i);

			if (!mon || !mon->race || !monster_is_visible(mon)
					|| monster_is_camouflaged(mon)) {
				continue;
			}
			net_jw_obj(w);
			net_jw_kstr(w, "name", mon->race->name);
			net_jw_kint(w, "x", mon->grid.x);
			net_jw_kint(w, "y", mon->grid.y);
			net_jw_kint(w, "health", mon->maxhp > 0 ?
				(100L * mon->hp) / mon->maxhp : 0);
			net_jw_kbool(w, "asleep", mon->m_timed[MON_TMD_SLEEP] > 0);
			net_jw_obj_end(w);
		}
	}
	net_jw_arr_end(w);
}

static void net_write_gear(struct net_jw *w)
{
	int i;

	net_jw_key(w, "inventory");
	net_jw_arr(w);
	for (i = 0; i < z_info->pack_size; i++) {
		struct object *obj = player->upkeep->inven[i];

		if (obj) net_write_object(w, obj, "inven", i);
	}
	net_jw_arr_end(w);

	net_jw_key(w, "equipment");
	net_jw_arr(w);
	for (i = 0; i < player->body.count; i++) {
		struct object *obj = slot_object(player, i);

		if (obj) net_write_object(w, obj, "equip", i);
	}
	net_jw_arr_end(w);

	net_jw_key(w, "quiver");
	net_jw_arr(w);
	for (i = 0; i < z_info->quiver_size; i++) {
		struct object *obj = player->upkeep->quiver[i];

		if (obj) net_write_object(w, obj, "quiver", i);
	}
	net_jw_arr_end(w);
}

/**
 * The objects under the player.
 */
static void net_write_floor(struct net_jw *w)
{
	struct object **floor;
	int i, n;

	net_jw_key(w, "floor");
	net_jw_arr(w);
	if (cave) {
		floor = mem_zalloc(z_info->floor_size * sizeof(*floor));
		n = net_floor_list(floor, z_info->floor_size);
		for (i = 0; i < n; i++) net_write_object(w, floor[i], "floor", i);
		mem_free(floor);
	}
	net_jw_arr_end(w);
}

/**
 * One character for a grid of the map the player knows.
 */
static char net_map_char(struct chunk *known, struct loc grid)
{
	if (square(known, grid)->feat == FEAT_NONE) return ' ';
	if (square_isvisibletrap(known, grid)) return '^';
	if (square_isdownstairs(known, grid)) return '>';
	if (square_isupstairs(known, grid)) return '<';
	if (square_isshop(known, grid)) return 's';
	if (square_iscloseddoor(known, grid)) return '+';
	if (square_isopendoor(known, grid) || square_isbrokendoor(known, grid))
		return '\'';
	if (square_isrubble(known, grid))
		return square_ispassable(known, grid) ? ':' : '#';
	return square_ispassable(known, grid) ? '.' : '#';
}

/**
 * The map as the player knows it, from player->cave, so it never shows
 * anything the player has not seen.
 */
static void net_write_map(struct net_jw *w)
{
	struct chunk *known = player->cave;
	char *row;
	int x, y;

	net_jw_key(w, "map");
	if (!known || !cave) {
		net_jw_null(w);
		return;
	}
	net_jw_obj(w);
	net_jw_kint(w, "width", known->width);
	net_jw_kint(w, "height", known->height);
	net_jw_key(w, "rows");
	net_jw_arr(w);
	row = mem_alloc(known->width);
	for (y = 0; y < known->height; y++) {
		for (x = 0; x < known->width; x++) {
			row[x] = net_map_char(known, loc(x, y));
		}
		net_jw_strn(w, row, known->width);
	}
	mem_free(row);
	net_jw_arr_end(w);
	net_jw_obj_end(w);
}

/**
 * ------------------------------------------------------------------------
 * The status part: what the sidebar and the status line show
 *
 * These follow the sidebar and status line functions in ui-display.c,
 * which draw the same things on the screen.  Keep the two in step.
 * ------------------------------------------------------------------------ */

static void net_span(struct net_jw *w, uint8_t color, const char *text)
{
	net_jw_obj(w);
	net_jw_kstr(w, "text", text);
	net_jw_kint(w, "color", color);
	net_jw_obj_end(w);
}

/* As prt_level_feeling() */
static void net_status_feeling(struct net_jw *w)
{
	static const uint8_t obj_color[] = {
		COLOUR_WHITE, COLOUR_L_PURPLE, COLOUR_L_RED, COLOUR_ORANGE,
		COLOUR_YELLOW, COLOUR_YELLOW, COLOUR_L_GREEN, COLOUR_L_GREEN,
		COLOUR_L_GREEN, COLOUR_L_BLUE, COLOUR_L_BLUE
	};
	static const uint8_t mon_color[] = {
		COLOUR_WHITE, COLOUR_RED, COLOUR_ORANGE, COLOUR_ORANGE,
		COLOUR_YELLOW, COLOUR_YELLOW, COLOUR_GREEN, COLOUR_GREEN,
		COLOUR_BLUE, COLOUR_BLUE
	};
	int obj_feeling, mon_feeling;
	char obj_str[6], mon_str[6];
	uint8_t obj_attr = COLOUR_WHITE;

	if (!OPT(player, birth_feelings) || !player->depth) return;
	obj_feeling = cave->feeling / 10;
	mon_feeling = cave->feeling - 10 * obj_feeling;
	if (cave->feeling_squares < z_info->feeling_need) {
		my_strcpy(obj_str, "?", sizeof(obj_str));
	} else {
		obj_attr = obj_color[obj_feeling];
		if (obj_feeling == 0) my_strcpy(obj_str, "*", sizeof(obj_str));
		else if (obj_feeling == 1) my_strcpy(obj_str, "$", sizeof(obj_str));
		else strnfmt(obj_str, sizeof(obj_str), "%d", 11 - obj_feeling);
	}
	if (mon_feeling == 0) my_strcpy(mon_str, "?", sizeof(mon_str));
	else strnfmt(mon_str, sizeof(mon_str), "%d", 10 - mon_feeling);
	net_span(w, COLOUR_WHITE, "LF:");
	net_span(w, mon_color[mon_feeling], mon_str);
	net_span(w, COLOUR_WHITE, "-");
	net_span(w, obj_attr, obj_str);
}

/* As prt_state() */
static void net_status_state(struct net_jw *w)
{
	char text[16] = "";

	if (player_is_resting(player)) {
		int n = player_resting_count(player);

		if (n > 0) strnfmt(text, sizeof(text), "Rest %d", n);
		else if (n == REST_ALL_POINTS) my_strcpy(text, "Rest *****", sizeof(text));
		else if (n == REST_COMPLETE) my_strcpy(text, "Rest &&&&&", sizeof(text));
		else if (n == REST_SOME_POINTS) my_strcpy(text, "Rest !!!!!", sizeof(text));
		else my_strcpy(text, "Rest", sizeof(text));
	} else if (cmd_get_nrepeats()) {
		strnfmt(text, sizeof(text), "Repeat %d", cmd_get_nrepeats());
	}
	if (text[0]) net_span(w, COLOUR_WHITE, text);
}

/* As prt_tmd(): one span per timed effect, and the food meter */
static void net_status_timed(struct net_jw *w)
{
	int i;

	for (i = 0; i < TMD_MAX; i++) {
		struct timed_grade *grade = timed_effects[i].grade;

		if (!player->timed[i]) continue;
		while (player->timed[i] > grade->max) grade = grade->next;
		if (!grade->name) continue;
		if (i == TMD_FOOD) {
			net_span(w, grade->color, format("%s %d %%", grade->name,
				player->timed[i] / 100));
		} else {
			net_span(w, grade->color, grade->name);
		}
	}
}

/*
 * The status line, as update_statusline() draws it: a list of spans, one
 * per item, in the game's order.  Spans hold no spaces between items.
 */
static void net_write_status_line(struct net_jw *w)
{
	int light = square_light(cave, player->grid);
	int moves = player->state.num_moves;
	struct trap *trap = square_trap(cave, player->grid);
	char buf[32];

	net_jw_key(w, "line");
	net_jw_arr(w);
	net_status_feeling(w);
	net_span(w, light > 0 ? COLOUR_YELLOW : COLOUR_PURPLE,
		format("Light %d", light));
	if (moves) net_span(w, COLOUR_L_TEAL, format("Moves %+d", moves));
	if (player->unignoring) net_span(w, COLOUR_WHITE, "Unignoring");
	if (player->word_recall) net_span(w, COLOUR_WHITE, "Recall");
	if (player->deep_descent) net_span(w, COLOUR_WHITE, "Descent");
	net_status_state(w);
	if (player->upkeep->new_spells) {
		net_span(w, player_book_has_unlearned_spells(player)
			? COLOUR_WHITE : COLOUR_L_DARK,
			format("Study (%d)", player->upkeep->new_spells));
	}
	net_status_timed(w);
	if (square_isdtrap(cave, player->grid)) {
		net_span(w, square_dtrap_edge(cave, player->grid)
			? COLOUR_YELLOW : COLOUR_L_GREEN, "DTrap");
	}
	if (trap && !square_isinvis(cave, player->grid)) {
		my_strcpy(buf, trap->kind->name, sizeof(buf));
		my_strcap(buf);
		net_span(w, trap->kind->d_attr, buf);
	} else {
		struct feature *feat = square_feat(cave, player->grid);

		my_strcpy(buf, feat->name, sizeof(buf));
		my_strcap(buf);
		net_span(w, feat->d_attr, buf);
	}
	net_jw_arr_end(w);
}

/* As fmt_title(), in its long form */
static const char *net_title(void)
{
	if (player->wizard) return "[=-WIZARD-=]";
	if (player->total_winner || player->lev > PY_MAX_LEVEL) {
		return "***WINNER***";
	}
	if (player_is_shapechanged(player)) {
		static char shape[32];

		my_strcpy(shape, player->shape->name, sizeof(shape));
		my_strcap(shape);
		return shape;
	}
	return player->class->title[(player->lev - 1) / 5];
}

/* As prt_speed_aux() */
static void net_write_speed(struct net_jw *w)
{
	int i = player->state.speed;
	const char *type = i > 110 ? "Fast" : i < 110 ? "Slow" : NULL;
	char buf[32] = "";

	if (type && OPT(player, effective_speed)) {
		int mul = 10 * extract_energy[i] / extract_energy[110];

		strnfmt(buf, sizeof(buf), "%s (%d.%dx)", type, mul / 10, mul % 10);
	} else if (type) {
		strnfmt(buf, sizeof(buf), "%s (%+d)", type, i - 110);
	}
	net_jw_kint(w, "speed", i - 110);
	net_jw_kstr(w, "speed_text", buf);
	net_jw_kint(w, "speed_color", i > 110 ? COLOUR_L_GREEN
		: i < 110 ? COLOUR_L_UMBER : COLOUR_WHITE);
}

/* As prt_health(): the tracked monster's health, or null */
static void net_write_health(struct net_jw *w)
{
	struct monster *mon = player->upkeep->health_who;

	net_jw_key(w, "health");
	if (!mon) {
		net_jw_null(w);
		return;
	}
	net_jw_obj(w);
	net_jw_kint(w, "color", monster_health_attr());
	if (!monster_is_visible(mon) || player->timed[TMD_IMAGE] || mon->hp < 0) {
		net_jw_kint(w, "percent", -1);
	} else {
		net_jw_kint(w, "percent", 100L * mon->hp / mon->maxhp);
	}
	net_jw_obj_end(w);
}

/* Encode one character as UTF-8, whatever the locale */
static void net_utf8(wchar_t c, char out[5])
{
	uint32_t cp = (uint32_t)c;

	if (cp < 0x80) {
		out[0] = (char)cp;
		out[1] = 0;
	} else if (cp < 0x800) {
		out[0] = (char)(0xC0 | (cp >> 6));
		out[1] = (char)(0x80 | (cp & 0x3F));
		out[2] = 0;
	} else if (cp < 0x10000) {
		out[0] = (char)(0xE0 | (cp >> 12));
		out[1] = (char)(0x80 | ((cp >> 6) & 0x3F));
		out[2] = (char)(0x80 | (cp & 0x3F));
		out[3] = 0;
	} else {
		out[0] = (char)(0xF0 | (cp >> 18));
		out[1] = (char)(0x80 | ((cp >> 12) & 0x3F));
		out[2] = (char)(0x80 | ((cp >> 6) & 0x3F));
		out[3] = (char)(0x80 | (cp & 0x3F));
		out[4] = 0;
	}
}

/*
 * As prt_equippy(): the letter for each equipment slot, or null.  With
 * tiles the game's letters are tile numbers, and headless mode loads no
 * letters at all, so those use each object's own letter and color from
 * the game data.  With tiles, each entry also has the object's tile.
 */
static void net_write_equippy(struct net_jw *w)
{
	int i;

	net_jw_key(w, "equippy");
	net_jw_arr(w);
	for (i = 0; i < player->body.count; i++) {
		struct object *obj = slot_object(player, i);
		struct object_kind *kind = obj ? obj->kind : NULL;
		wchar_t c;
		uint8_t a;
		char ch[5];

		if (!kind) {
			net_jw_null(w);
			continue;
		}
		if (use_graphics == GRAPHICS_NONE && !net_headless) {
			c = object_char(obj);
			a = object_attr(obj);
		} else if (kind->flavor
				&& !(kind->tval == TV_SCROLL && kind->aware)) {
			c = kind->flavor->d_char;
			a = kind->flavor->d_attr;
		} else {
			c = kind->d_char;
			a = kind->d_attr;
		}
		net_utf8(c, ch);
		net_jw_obj(w);
		net_jw_kstr(w, "char", ch);
		net_jw_kint(w, "color", a);
		if (use_graphics != GRAPHICS_NONE && !net_headless) {
			/* The object's tile, numbered as in PICT */
			net_jw_key(w, "tile");
			net_jw_obj(w);
			net_jw_kint(w, "row", object_attr(obj) & 0x7F);
			net_jw_kint(w, "col", object_char(obj) & 0x7F);
			net_jw_obj_end(w);
		}
		net_jw_obj_end(w);
	}
	net_jw_arr_end(w);
}

/**
 * Everything the sidebar and the status line show.
 */
static void net_write_status(struct net_jw *w)
{
	static const char *names[STAT_MAX] = { "STR", "INT", "WIS", "DEX", "CON" };
	bool magic = player->class->magic.total_spells
		&& player->lev >= player->class->magic.spell_first;
	int i;

	net_jw_key(w, "status");
	net_jw_obj(w);
	net_jw_kstr(w, "name", player->full_name);
	net_jw_kstr(w, "race", player_is_shapechanged(player) ? ""
		: player->race->name);
	net_jw_kstr(w, "class", player->class->name);
	net_jw_kstr(w, "title", net_title());
	net_jw_kint(w, "level", player->lev);
	net_jw_kint(w, "max_level", player->max_lev);
	net_jw_kint(w, "exp", player->exp);
	net_jw_kint(w, "max_exp", player->max_exp);
	/* Experience still needed for the next level, or -1 at level 50 */
	net_jw_kint(w, "next_exp", player->lev >= PY_MAX_LEVEL ? -1
		: (long)(player_exp[player->lev - 1] * player->expfact / 100L)
			- player->exp);
	net_jw_kint(w, "gold", player->au);
	net_write_equippy(w);
	net_jw_key(w, "stats");
	net_jw_arr(w);
	for (i = 0; i < STAT_MAX; i++) {
		net_jw_obj(w);
		net_jw_kstr(w, "name", names[i]);
		net_jw_kint(w, "value", player->state.stat_use[i]);
		net_jw_kbool(w, "drained", player->stat_cur[i] < player->stat_max[i]);
		net_jw_kbool(w, "maxed", player->stat_max[i] == 18 + 100);
		net_jw_obj_end(w);
	}
	net_jw_arr_end(w);
	net_jw_kint(w, "ac", player->known_state.ac + player->known_state.to_a);
	net_jw_kint(w, "hp", player->chp);
	net_jw_kint(w, "mhp", player->mhp);
	net_jw_kint(w, "hp_color", player_hp_attr(player));
	net_jw_kbool(w, "magic", magic);
	net_jw_kint(w, "sp", player->csp);
	net_jw_kint(w, "msp", player->msp);
	net_jw_kint(w, "sp_color", player_sp_attr(player));
	net_write_health(w);
	net_write_speed(w);
	net_jw_kint(w, "depth", player->depth);
	net_jw_kstr(w, "depth_text", player->depth
		? format("%d' (L%d)", player->depth * 50, player->depth) : "Town");
	net_write_status_line(w);
	net_jw_obj_end(w);
}

/**
 * The choices character creation offers, by index.
 */
static void net_write_birth(struct net_jw *w)
{
	int i;

	net_jw_key(w, "birth");
	net_jw_obj(w);
	net_jw_key(w, "races");
	net_jw_arr(w);
	for (i = 0; player_id2race(i); i++) net_jw_str(w, player_id2race(i)->name);
	net_jw_arr_end(w);
	net_jw_key(w, "classes");
	net_jw_arr(w);
	for (i = 0; player_id2class(i); i++) {
		net_jw_str(w, player_id2class(i)->name);
	}
	net_jw_arr_end(w);
	net_jw_obj_end(w);
}

/**
 * Send a snapshot with the given parts.  The turn, the context, and the
 * birth choices are always there.
 */
static void net_send_snapshot(unsigned parts)
{
	struct net_jw w;

	net_json_begin(&w, NET_SNAPSHOT);
	net_jw_kint(&w, "turn", turn);
	net_jw_kstr(&w, "context", context_name(net_ctx));
	if (net_ctx == CTX_BIRTH) net_write_birth(&w);
	if ((parts & PART_PLAYER)
			&& (character_generated || net_ctx == CTX_BIRTH)) {
		net_write_player(&w);
	}
	if (character_dungeon) {
		if (parts & PART_MONSTERS) net_write_monsters(&w);
		if (parts & PART_GEAR) net_write_gear(&w);
		if (parts & PART_FLOOR) net_write_floor(&w);
		if (parts & PART_MAP) net_write_map(&w);
		if (parts & PART_STATUS) net_write_status(&w);
	}
	net_json_end(&w);
}

/* The status part as last sent, to send it again only when it changes */
static char *last_status = NULL;
static size_t last_status_len = 0;

/**
 * With "status", send a snapshot with just the status part if it changed
 * since the last one.
 */
static void net_send_status(void)
{
	struct net_jw w;
	size_t frame, from, len;

	if (!cap_status || !character_dungeon || !cave || !player
			|| !player->race || !player->class) {
		return;
	}
	frame = net_out.len;
	net_json_begin(&w, NET_SNAPSHOT);
	net_jw_kint(&w, "turn", turn);
	net_jw_kstr(&w, "context", context_name(net_ctx));
	from = net_out.len;
	net_write_status(&w);
	len = net_out.len - from;
	if (len == last_status_len
			&& !memcmp(last_status, net_out.data + from, len)) {
		net_out.len = frame;
		return;
	}
	last_status = mem_realloc(last_status, len);
	memcpy(last_status, net_out.data + from, len);
	last_status_len = len;
	net_json_end(&w);
}

/**
 * ------------------------------------------------------------------------
 * Moves
 *
 * With "moves", the engine tells the client which creatures stepped where
 * since it last sent the screen, so the client can slide them from grid
 * to grid.  monster_swap() reports every move of the player and of the
 * monsters; only steps to a neighboring grid count.
 * ------------------------------------------------------------------------ */

static bool net_borg_playing(void);

/* Whether the game is acting by itself, too fast to animate */
static bool net_busy(void)
{
	return player->upkeep->running || player_is_resting(player)
		|| cmd_get_nrepeats() > 0 || net_borg_playing();
}

/* The longest path kept for one creature between two sends */
#define NET_MOVE_STEPS 8

struct net_move {
	int who;	/* A monster's index, or -1 for the player */
	struct loc path[NET_MOVE_STEPS];
	int n;
};

static struct net_move moves[64];
static int n_moves = 0;

static struct net_move *net_find_move(int who)
{
	int i;

	for (i = 0; i < n_moves; i++) {
		if (moves[i].who == who) return &moves[i];
	}
	return NULL;
}

/* Forget a creature's moves: it jumped, so it should not slide */
static void net_drop_move(int who)
{
	struct net_move *m = net_find_move(who);

	if (m) *m = moves[--n_moves];
}

static void net_note_move(int who, struct loc from, struct loc to)
{
	struct net_move *m = net_find_move(who);
	int dx = ABS(to.x - from.x), dy = ABS(to.y - from.y);

	if (dx > 1 || dy > 1 || (dx == 0 && dy == 0)) {
		net_drop_move(who);
		return;
	}
	if (m && !loc_eq(m->path[m->n - 1], from)) {
		/* Its path broke, as after a jump; start again here */
		m->n = 0;
	}
	if (!m) {
		if (n_moves == (int)N_ELEMENTS(moves)) return;
		m = &moves[n_moves++];
		m->who = who;
		m->n = 0;
	}
	if (m->n == 0) m->path[m->n++] = from;
	if (m->n == NET_MOVE_STEPS) {
		/* Keep the latest steps */
		memmove(m->path, m->path + 1, sizeof(m->path[0]) * (m->n - 1));
		m->n--;
	}
	m->path[m->n++] = to;
}

static void net_on_swap(struct loc grid1, struct loc grid2, int m1, int m2)
{
	if (!cap_moves || net_headless) return;
	if (m1) net_note_move(m1, grid1, grid2);
	if (m2) net_note_move(m2, grid2, grid1);
}

/*
 * The cell on the main term where the tile for a grid starts, or false
 * when the map does not show that grid.
 */
static bool net_grid_cell(struct loc grid, int *col, int *row)
{
	term *t = angband_term[0];
	int mode, wid, hgt;

	if (!t) return false;
	mode = t->sidebar_mode;
	wid = (t->wid - col_map[mode] - 1) / tile_width;
	hgt = (t->hgt - row_top_map[mode] - row_bottom_map[mode]) / tile_height;
	if (grid.x < t->offset_x || grid.y < t->offset_y
			|| grid.x >= t->offset_x + wid || grid.y >= t->offset_y + hgt) {
		return false;
	}
	*col = col_map[mode] + (grid.x - t->offset_x) * tile_width;
	*row = row_top_map[mode] + (grid.y - t->offset_y) * tile_height;
	return true;
}

/*
 * Whether the creature is on screen at the end of its path: the player,
 * or a monster the player sees there.
 */
static bool net_mover_shown(const struct net_move *m)
{
	struct loc end = m->path[m->n - 1];
	struct monster *mon;

	if (m->who < 0) return loc_eq(player->grid, end);
	mon = cave_monster(cave, m->who);
	return mon && mon->race && loc_eq(mon->grid, end)
		&& monster_is_visible(mon) && !monster_is_camouflaged(mon);
}

/**
 * Send the moves since the last send as a MOVES event: each creature's
 * path, in cells of the main term.  "busy" is set while the player runs,
 * rests, or repeats a command, or the borg plays, when the moves come too
 * fast to show.
 */
static void net_send_moves(void)
{
	struct net_jw w;
	int i, j, col, row, sent = 0;
	bool busy;

	if (!n_moves) return;
	if (!cap_moves || !character_dungeon || !cave || !player) {
		n_moves = 0;
		return;
	}
	busy = net_busy();
	for (i = 0; i < n_moves; i++) {
		const struct net_move *m = &moves[i];

		if (m->n < 2 || !net_mover_shown(m)) continue;
		for (j = 0; j < m->n; j++) {
			if (!net_grid_cell(m->path[j], &col, &row)) break;
		}
		if (j < m->n) continue;
		if (!sent) {
			net_json_begin(&w, NET_EVENT);
			net_jw_kstr(&w, "event", "MOVES");
			net_jw_kbool(&w, "busy", busy);
			net_jw_key(&w, "moves");
			net_jw_arr(&w);
		}
		sent++;
		net_jw_obj(&w);
		net_jw_key(&w, "path");
		net_jw_arr(&w);
		for (j = 0; j < m->n; j++) {
			net_grid_cell(m->path[j], &col, &row);
			net_jw_arr(&w);
			net_jw_int(&w, col);
			net_jw_int(&w, row);
			net_jw_arr_end(&w);
		}
		net_jw_arr_end(&w);
		net_jw_obj_end(&w);
	}
	if (sent) {
		net_jw_arr_end(&w);
		net_json_end(&w);
	}
	n_moves = 0;
}

/*
 * The level and the main term's layout when the engine last sent the
 * screen.  A scroll counts only on the same level with the same layout:
 * a new level, a resize, or a new tile size moves the map too, but the
 * screen before shows something else.
 */
static struct {
	struct chunk *cave;
	int32_t turn;
	int depth, mode, wid, hgt, tile_w, tile_h;
} shown_view;

static void net_view_now(void)
{
	term *t = angband_term[0];

	memset(&shown_view, 0, sizeof(shown_view));
	if (!t || !cave) return;
	shown_view.cave = cave;
	shown_view.turn = cave->turn;
	shown_view.depth = cave->depth;
	shown_view.mode = t->sidebar_mode;
	shown_view.wid = t->wid;
	shown_view.hgt = t->hgt;
	shown_view.tile_w = tile_width;
	shown_view.tile_h = tile_height;
}

/**
 * With "scrolls", tell the client the map is about to scroll, before the
 * game redraws it: a SCROLL event with "shift", how far the map's contents
 * move on the main term, in cells, and "map", the cells that show the
 * map.  The client keeps the screen it has, to scroll from it to the new
 * one.
 */
static void net_on_panel(int old_y, int old_x, int new_y, int new_x)
{
	term *t = angband_term[0];
	struct net_jw w;
	int mode;

	if (!cap_scrolls || net_headless || !t || !character_dungeon) return;
	mode = t->sidebar_mode;
	if (shown_view.cave != cave || shown_view.turn != cave->turn
			|| shown_view.depth != cave->depth || shown_view.mode != mode
			|| shown_view.wid != t->wid || shown_view.hgt != t->hgt
			|| shown_view.tile_w != tile_width
			|| shown_view.tile_h != tile_height) {
		return;
	}
	net_json_begin(&w, NET_EVENT);
	net_jw_kstr(&w, "event", "SCROLL");
	net_jw_kbool(&w, "busy", net_busy());
	net_jw_key(&w, "shift");
	net_jw_arr(&w);
	net_jw_int(&w, (old_x - new_x) * tile_width);
	net_jw_int(&w, (old_y - new_y) * tile_height);
	net_jw_arr_end(&w);
	net_jw_key(&w, "map");
	net_jw_obj(&w);
	net_jw_kint(&w, "x", col_map[mode]);
	net_jw_kint(&w, "y", row_top_map[mode]);
	net_jw_kint(&w, "w", ((t->wid - col_map[mode] - 1) / tile_width) * tile_width);
	net_jw_kint(&w, "h", ((t->hgt - row_top_map[mode] - row_bottom_map[mode])
		/ tile_height) * tile_height);
	net_jw_obj_end(&w);
	net_json_end(&w);
}

/**
 * main-net.c calls this before it sends what is buffered, so the moves
 * and the status go out with the screen they belong to.
 */
void net_game_before_flush(void)
{
	net_send_moves();
	net_send_status();
	net_view_now();
}

/**
 * ------------------------------------------------------------------------
 * Events
 * ------------------------------------------------------------------------ */

static const struct {
	game_event_type type;
	const char *name;
} forwarded_events[] = {
	{ EVENT_STATS, "STATS" },
	{ EVENT_HP, "HP" },
	{ EVENT_MANA, "MANA" },
	{ EVENT_AC, "AC" },
	{ EVENT_EXPERIENCE, "EXPERIENCE" },
	{ EVENT_PLAYERLEVEL, "PLAYERLEVEL" },
	{ EVENT_PLAYERTITLE, "PLAYERTITLE" },
	{ EVENT_GOLD, "GOLD" },
	{ EVENT_MONSTERHEALTH, "MONSTERHEALTH" },
	{ EVENT_DUNGEONLEVEL, "DUNGEONLEVEL" },
	{ EVENT_PLAYERSPEED, "PLAYERSPEED" },
	{ EVENT_RACE_CLASS, "RACE_CLASS" },
	{ EVENT_STUDYSTATUS, "STUDYSTATUS" },
	{ EVENT_STATUS, "STATUS" },
	{ EVENT_DETECTIONSTATUS, "DETECTIONSTATUS" },
	{ EVENT_FEELING, "FEELING" },
	{ EVENT_LIGHT, "LIGHT" },
	{ EVENT_STATE, "STATE" },
	{ EVENT_PLAYERMOVED, "PLAYERMOVED" },
	{ EVENT_SEEFLOOR, "SEEFLOOR" },
	{ EVENT_EXPLOSION, "EXPLOSION" },
	{ EVENT_BOLT, "BOLT" },
	{ EVENT_MISSILE, "MISSILE" },
	{ EVENT_INVENTORY, "INVENTORY" },
	{ EVENT_EQUIPMENT, "EQUIPMENT" },
	{ EVENT_ITEMLIST, "ITEMLIST" },
	{ EVENT_MONSTERLIST, "MONSTERLIST" },
	{ EVENT_MONSTERTARGET, "MONSTERTARGET" },
	{ EVENT_OBJECTTARGET, "OBJECTTARGET" },
	{ EVENT_MESSAGE, "MESSAGE" },
	{ EVENT_SOUND, "SOUND" },
	{ EVENT_BELL, "BELL" },
	{ EVENT_STORECHANGED, "STORECHANGED" },
	{ EVENT_NEW_LEVEL_DISPLAY, "NEW_LEVEL" },
	{ EVENT_CHEAT_DEATH, "CHEAT_DEATH" },
	{ EVENT_ENTER_BIRTH, "ENTER_BIRTH" },
	{ EVENT_LEAVE_BIRTH, "LEAVE_BIRTH" },
	{ EVENT_ENTER_GAME, "ENTER_GAME" },
	{ EVENT_LEAVE_GAME, "LEAVE_GAME" },
	{ EVENT_ENTER_WORLD, "ENTER_WORLD" },
	{ EVENT_LEAVE_WORLD, "LEAVE_WORLD" },
	{ EVENT_ENTER_STORE, "ENTER_STORE" },
	{ EVENT_LEAVE_STORE, "LEAVE_STORE" },
	{ EVENT_ENTER_DEATH, "ENTER_DEATH" },
	{ EVENT_LEAVE_DEATH, "LEAVE_DEATH" },
};

static void net_on_event(game_event_type type, game_event_data *data,
		void *user)
{
	const char *name = user;
	struct net_jw w;

	net_json_begin(&w, NET_EVENT);
	net_jw_kstr(&w, "event", name);
	switch (type) {
		case EVENT_HP:
			net_jw_kint(&w, "hp", player->chp);
			net_jw_kint(&w, "mhp", player->mhp);
			break;
		case EVENT_MANA:
			net_jw_kint(&w, "sp", player->csp);
			net_jw_kint(&w, "msp", player->msp);
			break;
		case EVENT_AC:
			net_jw_kint(&w, "ac",
				player->known_state.ac + player->known_state.to_a);
			break;
		case EVENT_EXPERIENCE:
		case EVENT_PLAYERLEVEL:
			net_jw_kint(&w, "level", player->lev);
			net_jw_kint(&w, "exp", player->exp);
			net_jw_kint(&w, "max_exp", player->max_exp);
			break;
		case EVENT_GOLD:
			net_jw_kint(&w, "gold", player->au);
			break;
		case EVENT_DUNGEONLEVEL:
			net_jw_kint(&w, "depth", player->depth);
			break;
		case EVENT_PLAYERSPEED:
			net_jw_kint(&w, "speed", player->state.speed - 110);
			break;
		case EVENT_PLAYERMOVED:
			net_jw_kint(&w, "x", player->grid.x);
			net_jw_kint(&w, "y", player->grid.y);
			break;
		case EVENT_MESSAGE:
			net_jw_kstr(&w, "text", data ? data->message.msg : NULL);
			net_jw_kint(&w, "type", data ? data->message.type : 0);
			break;
		case EVENT_SOUND:
			net_jw_kint(&w, "type", data ? data->message.type : 0);
			break;
		case EVENT_STATS: {
			int i;

			net_jw_key(&w, "stats");
			net_jw_obj(&w);
			for (i = 0; i < STAT_MAX; i++) {
				net_jw_kint(&w, stat_keys[i], player->state.stat_use[i]);
			}
			net_jw_obj_end(&w);
			break;
		}
		default:
			break;
	}
	net_json_end(&w);
}

static void net_apply_panels(bool redraw);

/**
 * Keep net_ctx up to date.  Context events go out as EVENT frames too
 * when the client asked for events.
 */
static void net_on_context(game_event_type type, game_event_data *data,
		void *user)
{
	switch (type) {
		case EVENT_ENTER_BIRTH: net_ctx = CTX_BIRTH; break;
		case EVENT_ENTER_GAME:
			/* The savefile has just set the sidebar mode */
			net_apply_panels(false);
			net_ctx = CTX_GAME;
			break;
		case EVENT_LEAVE_BIRTH:
		case EVENT_LEAVE_STORE: net_ctx = CTX_GAME; break;
		case EVENT_ENTER_STORE: net_ctx = CTX_STORE; break;
		case EVENT_ENTER_DEATH: net_ctx = CTX_DEATH; break;
		default: break;
	}
}

static const game_event_type context_events[] = {
	EVENT_ENTER_BIRTH, EVENT_LEAVE_BIRTH, EVENT_ENTER_GAME,
	EVENT_ENTER_STORE, EVENT_LEAVE_STORE, EVENT_ENTER_DEATH
};

static void net_use_store(game_event_type type, game_event_data *data,
		void *user);

/**
 * Headless mode: on a new level, ask for every status update, as the text
 * UI does, so a client hears the new depth, hit points and so on.
 */
static void net_new_level(game_event_type type, game_event_data *data,
		void *user)
{
	player->upkeep->update |= (PU_BONUS | PU_HP | PU_SPELLS | PU_TORCH
		| PU_UPDATE_VIEW | PU_DISTANCE);
	player->upkeep->redraw |= (PR_BASIC | PR_EXTRA | PR_MAP | PR_INVEN
		| PR_EQUIP | PR_MONSTER | PR_MONLIST | PR_ITEMLIST);
	handle_stuff(player);
}

/**
 * Add our event handlers.  The game drops every handler of the store
 * events each time the player uses a store, so this runs again before
 * each command; removing first keeps each handler registered once.
 */
static void net_register_events(void)
{
	size_t i;

	for (i = 0; i < N_ELEMENTS(context_events); i++) {
		event_remove_handler(context_events[i], net_on_context, NULL);
		event_add_handler(context_events[i], net_on_context, NULL);
	}
	if (cap_events) {
		for (i = 0; i < N_ELEMENTS(forwarded_events); i++) {
			void *name = (void *)forwarded_events[i].name;

			event_remove_handler(forwarded_events[i].type, net_on_event, name);
			event_add_handler(forwarded_events[i].type, net_on_event, name);
		}
	}
	if (net_headless) {
		event_remove_handler(EVENT_USE_STORE, net_use_store, NULL);
		event_add_handler(EVENT_USE_STORE, net_use_store, NULL);
		event_remove_handler(EVENT_NEW_LEVEL_DISPLAY, net_new_level, NULL);
		event_add_handler(EVENT_NEW_LEVEL_DISPLAY, net_new_level, NULL);
	}
}

/**
 * ------------------------------------------------------------------------
 * Requests and replies
 * ------------------------------------------------------------------------ */

static int request_id = 0;
static int reply_wanted = 0;
static struct net_json *reply = NULL;

/**
 * Start a REQUEST frame for a prompt.  The caller adds its own fields and
 * then calls net_request_wait().
 */
static void net_request_begin_named(struct net_jw *w, const char *kind,
		const char *text)
{
	net_json_begin(w, NET_REQUEST);
	net_jw_kint(w, "id", ++request_id);
	net_jw_kstr(w, "prompt", kind);
	net_jw_kstr(w, "text", text);
}

static void net_request_begin(struct net_jw *w, enum net_prompt kind,
		const char *text)
{
	net_request_begin_named(w, prompt_names[kind], text);
}

/**
 * Send the request and wait for its REPLY.  Returns the reply, which
 * the caller frees, or NULL if the player cancelled or the game is
 * exiting.
 */
static struct net_json *net_request_wait(struct net_jw *w)
{
	struct net_json *got;
	bool cancel = false;

	net_json_end(w);
	net_flush();
	if (net_is_exiting()) return NULL;

	reply_wanted = request_id;
	while (!reply && !net_is_exiting()) net_wait_input();
	reply_wanted = 0;
	got = reply;
	reply = NULL;
	if (got && net_json_get_bool(got, "cancel", &cancel) && cancel) {
		net_json_free(got);
		return NULL;
	}
	return got;
}

static void net_handle_reply(const struct net_frame *f)
{
	struct net_json *v = net_json_parse((const char *)f->payload, f->len);
	int id;

	if (v && reply_wanted && !reply && net_json_get_int(v, "id", &id)
			&& id == reply_wanted) {
		reply = v;
		return;
	}
	net_json_free(v);
}

/**
 * ------------------------------------------------------------------------
 * Prompt hooks
 * ------------------------------------------------------------------------ */

static bool net_get_check(const char *prompt)
{
	struct net_jw w;
	struct net_json *r;
	bool value = false;

	net_request_begin(&w, PROMPT_CHECK, prompt);
	r = net_request_wait(&w);
	if (r) net_json_get_bool(r, "value", &value);
	net_json_free(r);
	return value;
}

static bool net_get_string(const char *prompt, char *buf, size_t len)
{
	struct net_jw w;
	struct net_json *r;
	const char *value;
	bool ok = false;

	net_request_begin(&w, PROMPT_STRING, prompt);
	net_jw_kstr(&w, "default", buf);
	net_jw_kint(&w, "max", (long)len - 1);
	r = net_request_wait(&w);
	value = net_json_get_str(r, "value");
	if (value) {
		my_strcpy(buf, value, len);
		ok = true;
	}
	net_json_free(r);
	return ok;
}

static int net_get_quantity(const char *prompt, int max)
{
	struct net_jw w;
	struct net_json *r;
	int value = 0;

	net_request_begin(&w, PROMPT_QUANTITY, prompt);
	net_jw_kint(&w, "max", max);
	r = net_request_wait(&w);
	if (!net_json_get_int(r, "value", &value)) value = 0;
	net_json_free(r);
	return MAX(0, MIN(value, max));
}

static char net_get_char(const char *prompt, const char *options, size_t len,
		char fallback)
{
	struct net_jw w;
	struct net_json *r;
	const char *value;
	char c = fallback;

	net_request_begin(&w, PROMPT_CHAR, prompt);
	net_jw_key(&w, "options");
	net_jw_strn(&w, options, len);
	r = net_request_wait(&w);
	value = net_json_get_str(r, "value");
	if (value && value[0] && memchr(options, value[0], len)) c = value[0];
	net_json_free(r);
	return c;
}

static bool net_get_com(const char *prompt, char *command)
{
	struct net_jw w;
	struct net_json *r;
	const char *value;
	bool ok = false;

	net_request_begin(&w, PROMPT_COM, prompt);
	r = net_request_wait(&w);
	value = net_json_get_str(r, "value");
	if (value && value[0]) {
		*command = value[0];
		ok = true;
	}
	net_json_free(r);
	return ok;
}

/**
 * Ask for a direction, 1 to 9 as on the number pad.  allow_target admits
 * 5, which means the current target.
 */
static bool net_ask_dir(enum net_prompt kind, int *dir, bool allow_none,
		bool allow_target)
{
	struct net_jw w;
	struct net_json *r;
	int value;
	bool ok = false;

	net_request_begin(&w, kind, NULL);
	net_jw_kbool(&w, "allow_none", allow_none);
	r = net_request_wait(&w);
	if (net_json_get_int(r, "value", &value) && value >= 0 && value <= 9
			&& (value != 0 || allow_none)
			&& (value != 5 || allow_target || allow_none)) {
		*dir = value;
		ok = true;
	}
	net_json_free(r);
	return ok;
}

static bool net_get_rep_dir(int *dir, bool allow_none)
{
	return net_ask_dir(PROMPT_DIRECTION, dir, allow_none, false);
}

static bool net_get_aim_dir(int *dir)
{
	return net_ask_dir(PROMPT_AIM, dir, false, true);
}

static bool net_get_target_grid(int mode, struct loc *grid)
{
	struct net_jw w;
	struct net_json *r;
	int x, y;
	bool ok = false;

	net_request_begin(&w, PROMPT_GRID, NULL);
	net_jw_kint(&w, "mode", mode);
	r = net_request_wait(&w);
	if (net_json_get_int(r, "x", &x) && net_json_get_int(r, "y", &y)
			&& cave && square_in_bounds(cave, loc(x, y))) {
		*grid = loc(x, y);
		ok = true;
	}
	net_json_free(r);
	return ok;
}

static void net_write_spell(struct net_jw *w, int sidx, bool usable)
{
	const struct class_spell *spell = spell_by_index(player, sidx);

	net_jw_obj(w);
	net_jw_kint(w, "value", sidx);
	net_jw_kstr(w, "name", spell ? spell->name : NULL);
	net_jw_kint(w, "level", spell ? spell->slevel : 0);
	net_jw_kint(w, "mana", spell ? spell->smana : 0);
	net_jw_kbool(w, "usable", usable);
	net_jw_obj_end(w);
}

/**
 * Read a spell index from a reply and check it against the filter.
 */
static int net_spell_reply(struct net_json *r, bool (*filter)(const struct player *, int))
{
	int sidx = -1;

	if (!net_json_get_int(r, "value", &sidx) || sidx < 0
			|| !spell_by_index(player, sidx)
			|| (filter && !filter(player, sidx))) {
		sidx = -1;
	}
	net_json_free(r);
	return sidx;
}

static int net_get_spell_from_book(struct player *p, const char *verb,
		struct object *book, const char *error,
		bool (*spell_filter)(const struct player *p, int spell))
{
	struct net_jw w;
	int *spells = NULL;
	int i, n;

	net_request_begin(&w, PROMPT_SPELL_FROM_BOOK, NULL);
	net_jw_kstr(&w, "verb", verb);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	n = spell_collect_from_book(p, book, &spells);
	for (i = 0; i < n; i++) {
		net_write_spell(&w, spells[i], !spell_filter || spell_filter(p, spells[i]));
	}
	mem_free(spells);
	net_jw_arr_end(&w);
	return net_spell_reply(net_request_wait(&w), spell_filter);
}

/**
 * Ask for a spell from any book in the pack.  Each choice names its
 * book, which the game wants back with the spell.
 */
static int net_get_spell(struct player *p, const char *verb,
		item_tester book_filter, cmd_code cmd, const char *book_error,
		bool (*spell_filter)(const struct player *p, int spell),
		const char *spell_error, struct object **rtn_book)
{
	struct net_jw w;
	struct net_json *r;
	int i, j, n, sidx = -1;

	net_request_begin(&w, PROMPT_SPELL, NULL);
	net_jw_kstr(&w, "verb", verb);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	for (i = 0; i < z_info->pack_size; i++) {
		struct object *book = p->upkeep->inven[i];
		int *spells = NULL;

		if (!book || (book_filter && !book_filter(book))
				|| !player_object_to_book(p, book)) {
			continue;
		}
		n = spell_collect_from_book(p, book, &spells);
		for (j = 0; j < n; j++) {
			if (spell_filter && !spell_filter(p, spells[j])) continue;
			net_write_spell(&w, spells[j], true);
		}
		mem_free(spells);
	}
	net_jw_arr_end(&w);
	r = net_request_wait(&w);
	if (net_json_get_int(r, "value", &sidx) && sidx >= 0
			&& (!spell_filter || spell_filter(p, sidx))) {
		/* Find a book in the pack that holds the spell */
		for (i = 0; i < z_info->pack_size; i++) {
			struct object *book = p->upkeep->inven[i];
			int *spells = NULL;
			bool found = false;

			if (!book || (book_filter && !book_filter(book))
					|| !player_object_to_book(p, book)) {
				continue;
			}
			n = spell_collect_from_book(p, book, &spells);
			for (j = 0; j < n; j++) {
				if (spells[j] == sidx) found = true;
			}
			mem_free(spells);
			if (found) {
				if (rtn_book) *rtn_book = book;
				net_json_free(r);
				return sidx;
			}
		}
	}
	net_json_free(r);
	return -1;
}

static void net_item_choice(struct net_jw *w, struct object *obj,
		item_tester tester, const char *where, int index)
{
	if (obj && (!tester || tester(obj))) {
		net_write_object(w, obj, where, index);
	}
}

static bool net_get_item(struct object **choice, const char *pmt,
		const char *str, cmd_code cmd, item_tester tester, int mode)
{
	struct net_jw w;
	struct net_json *r;
	struct object *obj = NULL;
	int i;

	net_request_begin(&w, PROMPT_ITEM, pmt);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	if (mode & USE_INVEN) {
		for (i = 0; i < z_info->pack_size; i++) {
			net_item_choice(&w, player->upkeep->inven[i], tester, "inven", i);
		}
	}
	if (mode & USE_EQUIP) {
		for (i = 0; i < player->body.count; i++) {
			net_item_choice(&w, slot_object(player, i), tester, "equip", i);
		}
	}
	if (mode & USE_QUIVER) {
		for (i = 0; i < z_info->quiver_size; i++) {
			net_item_choice(&w, player->upkeep->quiver[i], tester, "quiver", i);
		}
	}
	if ((mode & USE_FLOOR) && cave) {
		struct object **floor = mem_zalloc(z_info->floor_size
			* sizeof(*floor));
		int n = net_floor_list(floor, z_info->floor_size);

		for (i = 0; i < n; i++) {
			net_item_choice(&w, floor[i], tester, "floor", i);
		}
		mem_free(floor);
	}
	net_jw_arr_end(&w);
	r = net_request_wait(&w);
	obj = r ? net_resolve_item(net_json_get(r, "item")) : NULL;
	net_json_free(r);
	if (!obj || (tester && !tester(obj))) return false;
	*choice = obj;
	return true;
}

static bool net_get_curse(int *choice, struct object *obj, char *dice_string)
{
	struct net_jw w;
	struct net_json *r;
	int i, value;
	bool ok = false;

	net_request_begin(&w, PROMPT_CURSE, NULL);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	for (i = 0; obj->curses && i < z_info->curse_max; i++) {
		if (obj->curses[i].power <= 0) continue;
		net_jw_obj(&w);
		net_jw_kint(&w, "value", i);
		net_jw_kstr(&w, "name", curses[i].name);
		net_jw_kint(&w, "power", obj->curses[i].power);
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	r = net_request_wait(&w);
	if (net_json_get_int(r, "value", &value) && obj->curses && value >= 0
			&& value < z_info->curse_max && obj->curses[value].power > 0) {
		*choice = value;
		ok = true;
	}
	net_json_free(r);
	return ok;
}

static int net_get_effect_from_list(const char *prompt, struct effect *effect,
		int count, bool allow_random)
{
	struct net_jw w;
	struct net_json *r;
	int value = -1;

	bool random = false;

	net_request_begin(&w, PROMPT_EFFECT, prompt);
	net_jw_kint(&w, "count", count);
	net_jw_kbool(&w, "allow_random", allow_random);
	r = net_request_wait(&w);
	/* -2 picks a random effect and -1 cancels; see game-input.c */
	if (r && allow_random && net_json_get_bool(r, "random", &random)
			&& random) {
		value = -2;
	} else if (!net_json_get_int(r, "value", &value) || value < 0
			|| (count >= 0 && value >= count)) {
		value = -1;
	}
	net_json_free(r);
	return value;
}

static bool net_confirm_debug(void)
{
	struct net_jw w;
	struct net_json *r;
	bool value = false;

	net_request_begin(&w, PROMPT_CONFIRM_DEBUG, NULL);
	r = net_request_wait(&w);
	if (r) net_json_get_bool(r, "value", &value);
	net_json_free(r);
	return value;
}

/**
 * The text UI's input hooks, as they were before the client claimed any,
 * so the borg can have them back while it plays.
 */
static struct {
	bool saved;
	bool (*get_string)(const char *prompt, char *buf, size_t len);
	int (*get_quantity)(const char *prompt, int max);
	bool (*get_check)(const char *prompt);
	char (*get_char)(const char *prompt, const char *options, size_t len,
		char fallback);
	bool (*get_com)(const char *prompt, char *command);
	bool (*get_rep_dir)(int *dir, bool allow_none);
	bool (*get_aim_dir)(int *dir);
	bool (*get_target_grid)(int mode, struct loc *grid);
	int (*get_spell_from_book)(struct player *p, const char *verb,
		struct object *book, const char *error,
		bool (*spell_filter)(const struct player *p, int spell));
	int (*get_spell)(struct player *p, const char *verb,
		item_tester book_filter, cmd_code cmd, const char *book_error,
		bool (*spell_filter)(const struct player *p, int spell),
		const char *spell_error, struct object **rtn_book);
	bool (*get_item)(struct object **choice, const char *pmt,
		const char *str, cmd_code cmd, item_tester tester, int mode);
	bool (*get_curse)(int *choice, struct object *obj, char *dice_string);
	int (*get_effect_from_list)(const char *prompt, struct effect *effect,
		int count, bool allow_random);
	bool (*confirm_debug)(void);
} text_hooks;

/**
 * Give the text UI back its input hooks.
 */
static void net_release_hooks(void)
{
	if (!text_hooks.saved) return;
	get_string_hook = text_hooks.get_string;
	get_quantity_hook = text_hooks.get_quantity;
	get_check_hook = text_hooks.get_check;
	get_char_hook = text_hooks.get_char;
	get_com_hook = text_hooks.get_com;
	get_rep_dir_hook = text_hooks.get_rep_dir;
	get_aim_dir_hook = text_hooks.get_aim_dir;
	get_target_grid_hook = text_hooks.get_target_grid;
	get_spell_from_book_hook = text_hooks.get_spell_from_book;
	get_spell_hook = text_hooks.get_spell;
	get_item_hook = text_hooks.get_item;
	get_curse_hook = text_hooks.get_curse;
	get_effect_from_list_hook = text_hooks.get_effect_from_list;
	confirm_debug_hook = text_hooks.confirm_debug;
}

/**
 * Point the input hooks the client claimed (all of them in headless mode)
 * at the request versions.
 */
static void net_claim_hooks(void)
{
	if (!text_hooks.saved) {
		text_hooks.saved = true;
		text_hooks.get_string = get_string_hook;
		text_hooks.get_quantity = get_quantity_hook;
		text_hooks.get_check = get_check_hook;
		text_hooks.get_char = get_char_hook;
		text_hooks.get_com = get_com_hook;
		text_hooks.get_rep_dir = get_rep_dir_hook;
		text_hooks.get_aim_dir = get_aim_dir_hook;
		text_hooks.get_target_grid = get_target_grid_hook;
		text_hooks.get_spell_from_book = get_spell_from_book_hook;
		text_hooks.get_spell = get_spell_hook;
		text_hooks.get_item = get_item_hook;
		text_hooks.get_curse = get_curse_hook;
		text_hooks.get_effect_from_list = get_effect_from_list_hook;
		text_hooks.confirm_debug = confirm_debug_hook;
	}
	if (claimed[PROMPT_STRING]) get_string_hook = net_get_string;
	if (claimed[PROMPT_QUANTITY]) get_quantity_hook = net_get_quantity;
	if (claimed[PROMPT_CHECK]) get_check_hook = net_get_check;
	if (claimed[PROMPT_CHAR]) get_char_hook = net_get_char;
	if (claimed[PROMPT_COM]) get_com_hook = net_get_com;
	if (claimed[PROMPT_DIRECTION]) get_rep_dir_hook = net_get_rep_dir;
	if (claimed[PROMPT_AIM]) get_aim_dir_hook = net_get_aim_dir;
	if (claimed[PROMPT_GRID]) get_target_grid_hook = net_get_target_grid;
	if (claimed[PROMPT_SPELL_FROM_BOOK]) {
		get_spell_from_book_hook = net_get_spell_from_book;
	}
	if (claimed[PROMPT_SPELL]) get_spell_hook = net_get_spell;
	if (claimed[PROMPT_ITEM]) get_item_hook = net_get_item;
	if (claimed[PROMPT_CURSE]) get_curse_hook = net_get_curse;
	if (claimed[PROMPT_EFFECT]) {
		get_effect_from_list_hook = net_get_effect_from_list;
	}
	if (claimed[PROMPT_CONFIRM_DEBUG]) confirm_debug_hook = net_confirm_debug;
}

/**
 * ------------------------------------------------------------------------
 * State shared by commands, screens, and the borg
 * ------------------------------------------------------------------------ */

/* Waiting in net_get_cmd(): headless, or the text UI's top-level wait */
static bool in_get_cmd = false;

/* A command was pushed for the current wait */
static bool command_ready = false;

/* The player asked to leave the store (headless mode) */
static bool leave_store = false;

/* A screen's REQUEST is waiting for its REPLY */
static bool screen_open = false;

static const char *net_push_command(const struct net_json *v);
static void net_command_result(int id, bool have_id, bool ok,
	const char *error);

/**
 * Whether the game waits for the player's next command at the top level,
 * with nothing open: the only safe moment for the text UI to do
 * something else.
 */
static bool net_at_prompt(void)
{
	return in_get_cmd && !command_ready && !screen_open
		&& net_ctx == CTX_GAME && screen_save_depth == 0;
}

/**
 * ------------------------------------------------------------------------
 * The borg
 *
 * The borg, Angband's automatic player, plays through the text UI: it
 * reads the screen and types keys.  While it plays, the engine hands no
 * screens or prompts to the client, and when it stops, the player's
 * options, which the borg changes, are put back.
 * ------------------------------------------------------------------------ */

#ifdef ALLOW_BORG
/* The borg's key hook, from ui-input.c, which declares it in no header */
extern struct keypress (*inkey_hack)(int flush_first);

/* borg_active when net_borg_check() last looked */
static bool borg_seen = false;

/* The player's options from before the BORG command started the borg */
static bool borg_opts_saved = false;
static struct player_options borg_saved_opts;
#endif

static bool net_borg_playing(void)
{
#ifdef ALLOW_BORG
	return borg_active;
#else
	return false;
#endif
}

static void net_screen_hooks(bool claim);

/**
 * Notice the borg starting or stopping, by itself or by command, and tell
 * the client.  Runs each time the game asks for a command.
 */
static void net_borg_check(void)
{
#ifdef ALLOW_BORG
	struct net_jw w;

	if (borg_active == borg_seen) return;
	borg_seen = borg_active;
	if (borg_active) {
		net_release_hooks();
		net_screen_hooks(false);
	} else {
		/* A stopped borg tidies up, restoring its changes to the ignore
		 * settings and flushing input, on its next key.  Have it do so
		 * now, before the client hears the game waits, so that flush
		 * cannot eat the player's first key. */
		if (inkey_hack) (void)inkey_hack(0);
		net_claim_hooks();
		net_screen_hooks(true);
		if (borg_opts_saved) {
			player->opts = borg_saved_opts;
			borg_opts_saved = false;
		}
	}

	net_json_begin(&w, NET_EVENT);
	net_jw_kstr(&w, "event", "BORG");
	net_jw_kbool(&w, "on", borg_active);
	net_json_end(&w);
#endif
}

#ifdef ALLOW_BORG
/**
 * The borg reads borg.txt from the user folder, and writes a short one
 * with default settings if there is none.  Copy the full one the build
 * puts in lib/customize instead, the first time.
 */
static void net_borg_settings(void)
{
	char user[1024], shipped[1024], line[1024];
	ang_file *in, *out;

	path_build(user, sizeof(user), ANGBAND_DIR_USER, "borg.txt");
	path_build(shipped, sizeof(shipped), ANGBAND_DIR_CUSTOMIZE, "borg.txt");
	if (file_exists(user)) return;
	in = file_open(shipped, MODE_READ, FTYPE_TEXT);
	if (!in) return;
	out = file_open(user, MODE_WRITE, FTYPE_TEXT);
	if (!out) {
		plog_fmt("net: cannot write %s", user);
		file_close(in);
		return;
	}
	while (file_getl(in, line, sizeof(line))) file_putf(out, "%s\n", line);
	file_close(out);
	file_close(in);
}
#endif

/**
 * BORG {"on": true} starts the borg as Ctrl+Z then 'z' would, without the
 * warnings, which the client shows itself.  {"on": false} stops it, even
 * in a store, where the borg ignores keys.
 */
static const char *net_borg_command(const struct net_json *v)
{
#ifdef ALLOW_BORG
	const struct net_json *args = net_json_get(v, "args");
	bool on = false;

	if (!args || !net_json_get_bool(args, "on", &on)) {
		return "args must give on";
	}
	if (net_headless) return "the borg needs the text UI";
	if (!on) {
		borg_active = false;
		return NULL;
	}
	if (borg_active) return NULL;
	/* Stopped, but net_borg_check() has not run its cleanup yet */
	if (borg_seen) return "the borg is still stopping";
	if (!character_generated || player->is_dead) return "no character";
	if (!net_at_prompt()) return "busy";

	net_borg_settings();
	borg_saved_opts = player->opts;
	borg_opts_saved = true;

	/* As do_cmd_try_borg() does once the player agrees */
	player->noscore |= NOSCORE_BORG;

	/* The borg may print notes as it starts; let them pass */
	player->opts.opt[OPT_auto_more] = true;

	/* Backslash skips keymaps, so the keys mean what they say */
	Term_keypress('\\', 0);
	Term_keypress(KTRL('Z'), 0);
	Term_keypress('z', 0);
	return NULL;
#else
	(void)v;
	return "this engine was built without the borg";
#endif
}

/**
 * ------------------------------------------------------------------------
 * Saving
 * ------------------------------------------------------------------------ */

static const char *net_save_command(void)
{
	if (!character_generated || player->is_dead) return "no character to save";
	if (net_headless) {
		if (!in_get_cmd) return "busy";
		return savefile_save(savefile) ? NULL : "saving failed";
	}
	if (!net_at_prompt() && !screen_open) return "busy";
	return save_game_checked() ? NULL : "saving failed";
}

/**
 * ------------------------------------------------------------------------
 * Queries
 *
 * A query reads game data, changes nothing, and is answered at once in
 * any context, even before the game starts.  The answer is an EVENT named
 * after the query, or a SNAPSHOT, carrying the query's id.
 * ------------------------------------------------------------------------ */

static void net_answer_begin(struct net_jw *w, const char *name,
		const struct net_json *v)
{
	int id;

	net_json_begin(w, NET_EVENT);
	net_jw_kstr(w, "event", name);
	if (net_json_get_int(v, "id", &id)) net_jw_kint(w, "id", id);
}

/**
 * SNAPSHOT, with {"parts": ["player", ...]} for some parts only.
 */
static void net_query_snapshot(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const struct net_json *list = args ? net_json_get(args, "parts") : NULL;
	unsigned parts = PART_ALL;

	if (list && list->type == NET_JSON_ARRAY) {
		const struct net_json *p;

		parts = 0;
		for (p = list->child; p; p = p->next) {
			if (p->type == NET_JSON_STRING) parts |= parse_parts(p->string);
		}
	}
	net_send_snapshot(parts);
}

/**
 * SAVEFILES lists the saved characters, as the -l option does.  "name" is
 * what -u takes; "current" marks the savefile this engine uses.  The
 * top-level "current" names it even before it is first written; it is
 * empty while a new character started with --new-save is being made.
 */
/**
 * Find the race and class in a living character's savefile description,
 * "Name, L5 Race Class, at DL3", as wr_description() writes it.
 */
static void net_save_race_class(const char *desc, const char **race,
		const char **cls)
{
	const struct player_race *r;
	const struct player_class *c;
	const char *end = NULL, *p;
	char want[160];
	size_t n;

	*race = *cls = NULL;
	if (!desc) return;
	for (p = strstr(desc, ", at DL"); p; p = strstr(p + 1, ", at DL")) end = p;
	if (!end) return;
	n = end - desc;
	for (r = races; r; r = r->next) {
		for (c = classes; c; c = c->next) {
			size_t len;

			strnfmt(want, sizeof(want), " %s %s", r->name, c->name);
			len = strlen(want);
			if (len <= n && strncmp(end - len, want, len) == 0) {
				*race = r->name;
				*cls = c->name;
				return;
			}
		}
	}
}

static void net_query_savefiles(const struct net_json *v)
{
	savefile_getter g = NULL;
	struct net_jw w;
	const char *current = strrchr(savefile, PATH_SEPC);
	const char *race, *cls;

	current = current ? current + 1 : savefile;
	net_answer_begin(&w, "SAVEFILES", v);
	/* The savefile may not exist yet, for a character not saved yet */
	net_jw_kstr(&w, "current", current);
	net_jw_key(&w, "saves");
	net_jw_arr(&w);
	while (got_savefile(&g)) {
		const struct savefile_details *d = get_savefile_details(g);

		net_jw_obj(&w);
		net_jw_kstr(&w, "name", d->fnam + d->foff);
		net_jw_kstr(&w, "description", d->desc);
		net_jw_kbool(&w, "dead", d->desc && strstr(d->desc, ", dead ("));
		net_jw_kbool(&w, "current", streq(d->fnam, current));
		net_save_race_class(d->desc, &race, &cls);
		if (race) net_jw_kstr(&w, "race", race);
		if (cls) net_jw_kstr(&w, "class", cls);
		net_jw_obj_end(&w);
	}
	cleanup_savefile_getter(g);
	net_jw_arr_end(&w);
	net_json_end(&w);
}

/* Option pages the client sees, by OPTIONS' page names */
static const struct {
	int page;
	const char *name;
} option_pages[] = {
	{ OP_INTERFACE, "interface" },
	{ OP_BIRTH, "birth" },
	{ OP_CHEAT, "cheat" },
	{ OP_SCORE, "score" },
};

/* The number settings, by name */
static const char *option_numbers[] = {
	"delay_factor", "hitpoint_warn", "lazymove_delay", "sidebar_mode",
};

static uint8_t *net_option_number(struct player_options *o, const char *name)
{
	if (streq(name, "delay_factor")) return &o->delay_factor;
	if (streq(name, "hitpoint_warn")) return &o->hitpoint_warn;
	if (streq(name, "lazymove_delay")) return &o->lazymove_delay;
	if (streq(name, "sidebar_mode")) return &o->sidebar_mode;
	return NULL;
}

/* Birth options change only while a character is being made */
static bool net_in_birth(void)
{
	return !character_generated || net_ctx == CTX_BIRTH;
}

/**
 * OPTIONS lists every interface, birth, and cheat option with its
 * description, page, value, and whether it can change now, and the number
 * settings.  The score options, which mark a character that has cheated,
 * come too, never editable.  Before a character loads, the values are the defaults for a
 * new character, and nothing can change.
 */
static void net_query_options(const struct net_json *v)
{
	struct player_options defaults;
	struct player_options *o = &player->opts;
	struct net_jw w;
	size_t p, i;
	int opt;
	bool loaded = character_generated || net_ctx == CTX_BIRTH;

	if (!loaded) {
		options_init_defaults(&defaults);
		o = &defaults;
	}
	net_answer_begin(&w, "OPTIONS", v);
	net_jw_kbool(&w, "loaded", loaded);
	net_jw_key(&w, "options");
	net_jw_arr(&w);
	for (p = 0; p < N_ELEMENTS(option_pages); p++) {
		for (opt = 0; opt < OPT_MAX; opt++) {
			if (option_type(opt) != option_pages[p].page) continue;
			net_jw_obj(&w);
			net_jw_kstr(&w, "name", option_name(opt));
			net_jw_kstr(&w, "description", option_desc(opt));
			net_jw_kstr(&w, "page", option_pages[p].name);
			net_jw_kbool(&w, "value", o->opt[opt]);
			net_jw_kbool(&w, "editable", loaded
				&& option_pages[p].page != OP_SCORE
				&& (option_pages[p].page != OP_BIRTH || net_in_birth()));
			net_jw_obj_end(&w);
		}
	}
	net_jw_arr_end(&w);
	net_jw_key(&w, "numbers");
	net_jw_obj(&w);
	for (i = 0; i < N_ELEMENTS(option_numbers); i++) {
		net_jw_kint(&w, option_numbers[i],
			*net_option_number(o, option_numbers[i]));
	}
	net_jw_obj_end(&w);

	/* The tile sets, for a client that offers to switch */
	net_jw_key(&w, "graphics");
	net_jw_obj(&w);
	net_jw_kint(&w, "set", use_graphics);
	net_jw_kint(&w, "scale", use_graphics ? tile_height : 1);
	net_jw_key(&w, "sets");
	net_jw_arr(&w);
	if (!net_headless && net_read_tile_sets()) {
		graphics_mode *mode;

		for (mode = graphics_modes; mode; mode = mode->pNext) {
			if (mode->grafID == GRAPHICS_NONE) continue;
			net_jw_obj(&w);
			net_jw_kint(&w, "id", mode->grafID);
			net_jw_kstr(&w, "name", mode->menuname);
			net_jw_obj_end(&w);
		}
	}
	net_jw_arr_end(&w);
	net_jw_obj_end(&w);
	net_json_end(&w);
}

static const struct {
	const char *name;
	void (*answer)(const struct net_json *v);
} queries[] = {
	{ "SNAPSHOT", net_query_snapshot },
	{ "SAVEFILES", net_query_savefiles },
	{ "OPTIONS", net_query_options },
};

/**
 * ------------------------------------------------------------------------
 * Settings
 *
 * Settings commands work at the command prompt and while a screen is
 * open.  They change how the game looks, so each redraws.
 * ------------------------------------------------------------------------ */

/**
 * Redraw everything, as do_cmd_redraw() does but without flushing input,
 * which would eat keys the player typed meanwhile.  The SDL2 front end
 * does the same after changing tiles.
 */
static void net_redraw(void)
{
	term *old = Term;

	if (net_headless || !character_dungeon) return;
	Term_activate(term_screen);
	event_signal_point(EVENT_MAP, -1, -1);
	verify_panel();
	player->upkeep->notice |= (PN_COMBINE);
	player->upkeep->update |= (PU_TORCH | PU_INVEN | PU_BONUS | PU_HP
		| PU_SPELLS | PU_UPDATE_VIEW | PU_MONSTERS);
	player->upkeep->redraw |= (PR_BASIC | PR_EXTRA | PR_MAP | PR_INVEN
		| PR_EQUIP | PR_MESSAGE | PR_MONSTER | PR_OBJECT | PR_MONLIST
		| PR_ITEMLIST);
	Term_clear();
	handle_stuff(player);
	move_cursor_relative(player->grid.x, player->grid.y);
	Term_redraw_all();
	Term_activate(old);
}

/* Settings change at the prompt or in a screen, not in a C menu */
static bool net_settings_allowed(void)
{
	return net_headless ? in_get_cmd : (net_at_prompt() || screen_open);
}

/**
 * SET_OPTION {"name": NAME, "value": V} sets an option, true or false, or
 * a number setting.  Birth options change only during character creation;
 * a cheat option also marks the character, as the C menu does.
 */
static const char *net_set_option_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "name") : NULL;
	uint8_t *number;
	int opt, value = 0;
	bool on = false;

	if (!name) return "args must give name";
	if (!character_generated && net_ctx != CTX_BIRTH) return "no character";
	if (!net_settings_allowed()) return "busy";

	number = net_option_number(&player->opts, name);
	if (number) {
		if (!net_json_get_int(args, "value", &value)) {
			return "value must be a number";
		}
		if (streq(name, "hitpoint_warn") && (value < 0 || value > 9)) {
			return "hitpoint_warn must be 0 to 9";
		}
		if (streq(name, "sidebar_mode")
				&& (value < 0 || value >= SIDEBAR_MAX)) {
			return "no such sidebar mode";
		}
		if (value < 0 || value > 255) return "value must be 0 to 255";
		*number = (uint8_t)value;
		/* While PANELS hides the sidebar, the mode waits in the options */
		if (streq(name, "sidebar_mode")) net_apply_panels(false);
		net_redraw();
		return NULL;
	}

	for (opt = 0; opt < OPT_MAX; opt++) {
		if (option_name(opt) && streq(option_name(opt), name)) break;
	}
	if (opt == OPT_MAX) return "no such option";
	switch (option_type(opt)) {
		case OP_INTERFACE: case OP_CHEAT: break;
		case OP_BIRTH:
			if (!net_in_birth()) return "birth options change only at birth";
			break;
		default: return "that option cannot be set";
	}
	if (!net_json_get_bool(args, "value", &on)) return "value must be true or false";
	option_set(name, on);
	net_redraw();
	return NULL;
}

/**
 * SAVE_OPTION_DEFAULTS {"page": "interface" or "birth"} saves that page of
 * the character's options as the defaults for new characters.
 */
static const char *net_save_defaults_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *page = args ? net_json_get_str(args, "page") : NULL;
	int p;

	if (!page) return "args must give page";
	if (streq(page, "interface")) p = OP_INTERFACE;
	else if (streq(page, "birth")) p = OP_BIRTH;
	else return "page must be interface or birth";
	if (!character_generated && net_ctx != CTX_BIRTH) return "no character";
	return options_save_custom(&player->opts, p) ? NULL : "saving failed";
}

/**
 * GRAPHICS {"set": N, "scale": S} draws the map with tile set N, or with
 * text for 0, at S times the usual size, sends TILESET, and redraws.
 */
static const char *net_graphics_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	int id = 0, scale = 1;
	const char *error;

	if (!args || !net_json_get_int(args, "set", &id)) return "args must give set";
	net_json_get_int(args, "scale", &scale);
	if (net_headless) return "headless mode has no screen";
	if (id < 0) return "no such tile set";
	if (!net_settings_allowed() && character_generated) return "busy";
	error = net_set_tiles(id, scale);
	if (error) return error;
	if (character_dungeon) reset_visuals(true);
	net_redraw();
	return NULL;
}

/* PANELS hides the sidebar; the savefile keeps the player's own mode */
static bool sidebar_hidden = false;

/**
 * Put the sidebar mode the player chose, or none while PANELS hides the
 * sidebar, on the main term.  It never changes the mode in the savefile,
 * so other front ends still show the sidebar.
 */
static void net_apply_panels(bool redraw)
{
	int want;

	if (net_headless || !character_generated) return;
	want = player->opts.sidebar_mode < SIDEBAR_MAX
		? player->opts.sidebar_mode : SIDEBAR_LEFT;
	if (sidebar_hidden) want = SIDEBAR_NONE;
	if (SIDEBAR_MODE == want) return;
	SIDEBAR_MODE = want;
	if (redraw) net_redraw();
}

/**
 * PANELS {"sidebar": false, "status_line": false} stops the game drawing
 * its sidebar and its status line, for a client that draws them itself.
 * true draws them again.  It works before a character loads too.
 */
static const char *net_panels_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	bool on;

	if (!args) return "args must give sidebar or status_line";
	if (net_headless) return "headless mode has no screen";
	if (!net_settings_allowed() && character_generated) return "busy";
	if (net_json_get_bool(args, "sidebar", &on)) sidebar_hidden = !on;
	if (net_json_get_bool(args, "status_line", &on)) statusline_hidden = !on;
	net_apply_panels(false);
	net_redraw();
	return NULL;
}

/**
 * ------------------------------------------------------------------------
 * Screens
 *
 * For each screen the client claims, the text UI's command for it sends a
 * REQUEST with "prompt": "screen" and waits.  The REPLY is {} to close the
 * screen, {"then": COMMAND} to close it and run a command, or
 * {"classic": true} to show the text UI's own screen instead.  Swapping
 * the commands' functions in the text UI's tables means a screen opens
 * however the player asks for it: by key, keymap, or menu.
 * ------------------------------------------------------------------------ */

static void net_screen(enum net_screen screen, const char *view,
		void (*classic)(void))
{
	struct net_jw w;
	struct net_json *r;
	const struct net_json *then;
	bool use_classic = false;

	if (net_borg_playing() || net_is_exiting()) {
		classic();
		return;
	}

	net_request_begin_named(&w, "screen", NULL);
	net_jw_kstr(&w, "name", screen_names[screen]);
	if (view) net_jw_kstr(&w, "view", view);
	screen_open = true;
	r = net_request_wait(&w);
	screen_open = false;
	if (!r) return;

	if (net_json_get_bool(r, "classic", &use_classic) && use_classic) {
		net_json_free(r);
		classic();
		return;
	}
	then = net_json_get(r, "then");
	if (then && then->type == NET_JSON_OBJECT) {
		const char *error = net_push_command(then);
		int id = 0;
		bool have_id = net_json_get_int(then, "id", &id);

		net_command_result(id, have_id, error == NULL, error);
	}
	net_json_free(r);
}

#define NET_SCREEN_HOOK(fn, screen, view) \
	static void net_screen_##fn(void) { net_screen(screen, view, fn); }

NET_SCREEN_HOOK(do_cmd_xxx_options, SCREEN_OPTIONS, NULL)
NET_SCREEN_HOOK(do_cmd_inven, SCREEN_INVENTORY, "inven")
NET_SCREEN_HOOK(do_cmd_equip, SCREEN_INVENTORY, "equip")
NET_SCREEN_HOOK(do_cmd_quiver, SCREEN_INVENTORY, "quiver")
NET_SCREEN_HOOK(do_cmd_look, SCREEN_LOOK, NULL)
NET_SCREEN_HOOK(textui_target, SCREEN_TARGET, NULL)
NET_SCREEN_HOOK(textui_browse_knowledge, SCREEN_KNOWLEDGE, NULL)
NET_SCREEN_HOOK(do_cmd_change_name, SCREEN_CHARACTER, NULL)
NET_SCREEN_HOOK(do_cmd_messages, SCREEN_MESSAGES, NULL)
NET_SCREEN_HOOK(do_cmd_help, SCREEN_HELP, NULL)
NET_SCREEN_HOOK(do_cmd_view_map, SCREEN_MAP, NULL)
NET_SCREEN_HOOK(death_screen, SCREEN_DEATH, NULL)

static const struct {
	enum net_screen screen;
	void (*classic)(void);
	void (*hook)(void);
} screen_hooks[] = {
	{ SCREEN_OPTIONS, do_cmd_xxx_options, net_screen_do_cmd_xxx_options },
	{ SCREEN_INVENTORY, do_cmd_inven, net_screen_do_cmd_inven },
	{ SCREEN_INVENTORY, do_cmd_equip, net_screen_do_cmd_equip },
	{ SCREEN_INVENTORY, do_cmd_quiver, net_screen_do_cmd_quiver },
	{ SCREEN_LOOK, do_cmd_look, net_screen_do_cmd_look },
	{ SCREEN_TARGET, textui_target, net_screen_textui_target },
	{ SCREEN_KNOWLEDGE, textui_browse_knowledge,
		net_screen_textui_browse_knowledge },
	{ SCREEN_CHARACTER, do_cmd_change_name, net_screen_do_cmd_change_name },
	{ SCREEN_MESSAGES, do_cmd_messages, net_screen_do_cmd_messages },
	{ SCREEN_HELP, do_cmd_help, net_screen_do_cmd_help },
	{ SCREEN_MAP, do_cmd_view_map, net_screen_do_cmd_view_map },
};

/**
 * Point the text UI's commands for the claimed screens at the hand-over
 * functions, or back at their own.  The death screen has a hook of its
 * own, since no command opens it.
 */
static void net_screen_hooks(bool claim)
{
	const struct command_list *l;
	size_t i, j;

	for (l = cmds_all; l->name; l++) {
		for (i = 0; i < l->len; i++) {
			struct cmd_info *c = &l->list[i];

			for (j = 0; j < N_ELEMENTS(screen_hooks); j++) {
				if (!screen_claimed[screen_hooks[j].screen]) continue;
				if (claim && c->hook == screen_hooks[j].classic) {
					c->hook = screen_hooks[j].hook;
				} else if (!claim && c->hook == screen_hooks[j].hook) {
					c->hook = screen_hooks[j].classic;
				}
			}
		}
	}
	if (screen_claimed[SCREEN_DEATH]) {
		death_screen_hook = claim ? net_screen_death_screen : NULL;
	}
}

/**
 * ------------------------------------------------------------------------
 * Commands
 * ------------------------------------------------------------------------ */

/**
 * Command names, indexed by cmd_code.  They are the enum names without
 * "CMD_".  The array size check below fails the build if cmd-core.h
 * gains or loses a command.
 */
static const char *cmd_names[] = {
	"NULL", "LOADFILE", "NEWGAME", "BIRTH_INIT", "BIRTH_RESET",
	"CHOOSE_RACE", "CHOOSE_CLASS", "BUY_STAT", "SELL_STAT", "RESET_STATS",
	"REFRESH_STATS", "ROLL_STATS", "PREV_STATS", "NAME_CHOICE",
	"HISTORY_CHOICE", "ACCEPT_CHARACTER", "GO_UP", "GO_DOWN", "WALK",
	"JUMP", "PATHFIND", "INSCRIBE", "UNINSCRIBE", "AUTOINSCRIBE",
	"TAKEOFF", "WIELD", "DROP", "BROWSE_SPELL", "STUDY", "CAST",
	"USE_STAFF", "USE_WAND", "USE_ROD", "ACTIVATE", "EAT", "QUAFF",
	"READ_SCROLL", "REFILL", "USE", "FIRE", "THROW", "PICKUP",
	"AUTOPICKUP", "IGNORE", "DISARM", "REST", "TUNNEL", "OPEN", "CLOSE",
	"RUN", "EXPLORE", "NAVIGATE_UP", "NAVIGATE_DOWN", "HOLD", "ALTER",
	"STEAL", "SLEEP", "SELL", "BUY", "STASH", "RETRIEVE",
	"SPOIL_ARTIFACT", "SPOIL_MON", "SPOIL_MON_BRIEF", "SPOIL_OBJ",
	"WIZ_ACQUIRE", "WIZ_ADVANCE", "WIZ_BANISH",
	"WIZ_CHANGE_ITEM_QUANTITY", "WIZ_COLLECT_DISCONNECT_STATS",
	"WIZ_COLLECT_OBJ_MON_STATS", "WIZ_COLLECT_PIT_STATS",
	"WIZ_CREATE_ALL_ARTIFACT", "WIZ_CREATE_ALL_ARTIFACT_FROM_TVAL",
	"WIZ_CREATE_ALL_OBJ", "WIZ_CREATE_ALL_OBJ_FROM_TVAL",
	"WIZ_CREATE_ARTIFACT", "WIZ_CREATE_OBJ", "WIZ_CREATE_TRAP",
	"WIZ_CURE_ALL", "WIZ_CURSE_ITEM", "WIZ_DETECT_ALL_LOCAL",
	"WIZ_DETECT_ALL_MONSTERS", "WIZ_DISPLAY_KEYLOG", "WIZ_DUMP_LEVEL_MAP",
	"WIZ_EDIT_PLAYER_EXP", "WIZ_EDIT_PLAYER_GOLD", "WIZ_EDIT_PLAYER_START",
	"WIZ_EDIT_PLAYER_STAT", "WIZ_HIT_ALL_LOS", "WIZ_INCREASE_EXP",
	"WIZ_JUMP_LEVEL", "WIZ_LEARN_OBJECT_KINDS", "WIZ_MAGIC_MAP",
	"WIZ_PEEK_NOISE_SCENT", "WIZ_PERFORM_EFFECT", "WIZ_PLAY_ITEM",
	"WIZ_PUSH_OBJECT", "WIZ_QUERY_FEATURE", "WIZ_QUERY_SQUARE_FLAG",
	"WIZ_QUIT_NO_SAVE", "WIZ_RECALL_MONSTER", "WIZ_RERATE",
	"WIZ_REROLL_ITEM", "WIZ_STAT_ITEM", "WIZ_SUMMON_NAMED",
	"WIZ_SUMMON_RANDOM", "WIZ_TELEPORT_RANDOM", "WIZ_TELEPORT_TO",
	"WIZ_TWEAK_ITEM", "WIZ_WIPE_RECALL", "WIZ_WIZARD_LIGHT", "RETIRE",
	"HELP", "REPEAT", "COMMAND_MONSTER"
};

typedef char cmd_names_match_cmd_core_h
	[(N_ELEMENTS(cmd_names) == CMD_COMMAND_MONSTER + 1) ? 1 : -1];

/**
 * The context a command may be sent in, or CTX_INIT for commands a
 * client may not send: the splash screen, spoiler, debug and help
 * commands belong to the text UI or to wizard mode.
 */
static cmd_context command_context(cmd_code code)
{
	if (code >= CMD_BIRTH_INIT && code <= CMD_ACCEPT_CHARACTER) return CTX_BIRTH;
	if (code >= CMD_GO_UP && code <= CMD_SLEEP) return CTX_GAME;
	if (code >= CMD_SELL && code <= CMD_RETRIEVE) return CTX_STORE;
	if (code == CMD_RETIRE || code == CMD_REPEAT
			|| code == CMD_COMMAND_MONSTER) {
		return CTX_GAME;
	}
	return CTX_INIT;
}

static bool command_by_name(const char *name, cmd_code *code)
{
	size_t i;

	for (i = 1; i < N_ELEMENTS(cmd_names); i++) {
		if (streq(name, cmd_names[i])) {
			*code = (cmd_code)i;
			return true;
		}
	}
	return false;
}

/* Argument names that take a number rather than a choice */
static const char *number_args[] = {
	"quantity", "index", "tval", "range", "level", "power", "depth",
	"depth_min", "depth_max"
};

/**
 * Set a command's arguments from the "args" object of a COMMAND.  The
 * JSON shape and the name pick the type: an object with "x" and "y" is a
 * point, any other object an item reference, a string a string,
 * "direction" and "target" their own types, the names in number_args a
 * number, and any other whole number a choice.
 */
static bool net_set_args(struct command *cmd, const struct net_json *args,
		const char **error)
{
	const struct net_json *a;

	if (!args) return true;
	if (args->type != NET_JSON_OBJECT) {
		*error = "args must be an object";
		return false;
	}
	for (a = args->child; a; a = a->next) {
		const char *name = a->key;
		int n, x, y;
		size_t i;

		if (strlen(name) >= sizeof(cmd->arg[0].name)) {
			*error = "argument name too long";
			return false;
		}
		if (a->type == NET_JSON_OBJECT) {
			if (net_json_get_int(a, "x", &x) && net_json_get_int(a, "y", &y)) {
				cmd_set_arg_point(cmd, name, loc(x, y));
			} else {
				struct object *obj = net_resolve_item(a);

				if (!obj) {
					*error = "no such item";
					return false;
				}
				cmd_set_arg_item(cmd, name, obj);
			}
		} else if (a->type == NET_JSON_STRING) {
			cmd_set_arg_string(cmd, name, a->string);
		} else if (net_json_int(a, &n)) {
			if (streq(name, "direction")) {
				if (n < 0 || n > 9) {
					*error = "direction must be 0 to 9";
					return false;
				}
				cmd_set_arg_direction(cmd, name, n);
			} else if (streq(name, "target")) {
				cmd_set_arg_target(cmd, name, n);
			} else {
				bool number = false;

				for (i = 0; i < N_ELEMENTS(number_args); i++) {
					if (streq(name, number_args[i])) number = true;
				}
				if (number) {
					cmd_set_arg_number(cmd, name, n);
				} else {
					cmd_set_arg_choice(cmd, name, n);
				}
			}
		} else {
			*error = "unsupported argument value";
			return false;
		}
	}
	return true;
}

static void net_command_result(int id, bool have_id, bool ok,
		const char *error)
{
	struct net_jw w;

	net_json_begin(&w, NET_EVENT);
	net_jw_kstr(&w, "event", "COMMAND");
	if (have_id) net_jw_kint(&w, "id", id);
	net_jw_kbool(&w, "ok", ok);
	if (error) net_jw_kstr(&w, "error", error);
	net_json_end(&w);
	net_flush();
}

/**
 * Check a command and push it onto the command queue, for a COMMAND frame
 * or a screen's "then".  Returns NULL on success or why it was refused.
 */
static const char *net_push_command(const struct net_json *v)
{
	const char *name = net_json_get_str(v, "cmd");
	const char *error = NULL;
	cmd_code code;
	cmd_context ctx;
	int repeat;

	if (!name) return "missing cmd";
	if (!command_by_name(name, &code)) return "unknown command";
	ctx = command_context(code);
	if (ctx == CTX_INIT) return "command not allowed";
	if (ctx != net_ctx) return "command not allowed here";

	if (cmdq_push(code)) return "the command queue is full";
	if (!net_set_args(cmdq_peek(), net_json_get(v, "args"), &error)) {
		cmdq_flush();
		return error;
	}
	if (net_json_get_int(v, "repeat", &repeat) && repeat > 0) {
		cmd_set_repeat(MIN(repeat, 9999));
	}
	return NULL;
}

/**
 * Handle a COMMAND: answer a query, change a setting, or push a game
 * command.  Returns NULL on success or why it was refused.
 */
static const char *net_run_command(const struct net_json *v)
{
	const char *name = net_json_get_str(v, "cmd");
	const char *error;
	cmd_code code;
	cmd_context ctx;
	size_t i;

	if (!name) return "missing cmd";

	for (i = 0; i < N_ELEMENTS(queries); i++) {
		if (streq(name, queries[i].name)) {
			queries[i].answer(v);
			return NULL;
		}
	}
	if (streq(name, "SAVE")) return net_save_command();
	if (streq(name, "SET_OPTION")) return net_set_option_command(v);
	if (streq(name, "SAVE_OPTION_DEFAULTS")) return net_save_defaults_command(v);
	if (streq(name, "GRAPHICS")) return net_graphics_command(v);
	if (streq(name, "PANELS")) return net_panels_command(v);
	if (streq(name, "BORG")) return net_borg_command(v);
	if (streq(name, "LEAVE_STORE")) {
		if (!net_headless || net_ctx != CTX_STORE || !in_get_cmd) {
			return "not in a store";
		}
		leave_store = true;
		command_ready = true;
		return NULL;
	}

	if (!command_by_name(name, &code)) return "unknown command";
	ctx = command_context(code);
	if (ctx == CTX_INIT) return "command not allowed";
	if (ctx != net_ctx) return "command not allowed here";
	if (!in_get_cmd || command_ready || screen_open) return "busy";
	if (!net_headless && (net_ctx != CTX_GAME || screen_save_depth > 0)) {
		/* The text UI has a menu open; only its top-level wait is safe */
		return "busy";
	}

	error = net_push_command(v);
	if (error) return error;
	command_ready = true;

	/* Wake the text UI's wait; it redraws on a resize event and returns,
	 * and then the game runs the queued command */
	if (!net_headless) {
		ui_event e = EVENT_EMPTY;

		e.type = EVT_RESIZE;
		Term_event_push(&e);
	}
	return NULL;
}

/**
 * Handle a COMMAND or REPLY frame from the client.
 */
void net_game_frame(const struct net_frame *f)
{
	struct net_json *v;
	const char *error;
	int id = 0;
	bool have_id;

	if (f->type == NET_REPLY) {
		net_handle_reply(f);
		return;
	}
	if (f->type != NET_COMMAND) return;

	v = net_json_parse((const char *)f->payload, f->len);
	if (!v || v->type != NET_JSON_OBJECT) {
		net_json_free(v);
		net_command_result(0, false, false, "invalid JSON");
		return;
	}
	have_id = net_json_get_int(v, "id", &id);
	error = net_run_command(v);
	net_command_result(id, have_id, error == NULL, error);
	net_json_free(v);
}

/**
 * ------------------------------------------------------------------------
 * Waiting for commands
 * ------------------------------------------------------------------------ */

/* The text UI's command hook, which net_get_cmd() wraps */
static errr (*text_get_cmd)(cmd_context context) = NULL;

/**
 * Tell the client the game waits for a command, in the given context.
 */
static void net_send_wait(const char *context)
{
	struct net_jw w;

	net_json_begin(&w, NET_EVENT);
	net_jw_kstr(&w, "event", "WAIT");
	net_jw_kstr(&w, "context", context);
	net_json_end(&w);
}

static bool splash_waited = false;

/**
 * The text UI waits for a key.  The first wait before a character is
 * loaded or made is the splash screen (main.c's pause_line()), which a
 * client with "waits" hears about once.
 */
void net_game_key_wait(void)
{
	if (cap_waits && !net_headless && net_ctx == CTX_INIT && !splash_waited) {
		splash_waited = true;
		net_send_wait("splash");
	}
}

/**
 * The game asks for its next command.  In headless mode, wait for a
 * COMMAND; with the text UI, let it read keys as usual, but accept a
 * COMMAND while it waits.
 */
static errr net_get_cmd(cmd_context context)
{
	errr result = 0;

	net_register_events();
	net_borg_check();
	/* The classic options menu may have changed the sidebar mode */
	net_apply_panels(true);
	if (cap_snapshots && !net_borg_playing()) {
		net_send_snapshot(snapshot_parts);
	}

	/* The last command's moves and status go out with its screen, before
	 * the WAIT, not after the next key */
	net_game_before_flush();

	in_get_cmd = true;
	command_ready = false;
	if (net_headless) {
		net_send_wait(context_name(net_ctx));
		net_flush();
		while (!command_ready && !net_is_exiting()) net_wait_input();
	} else {
		/* The text UI's own menus never reach here, so this is the top
		 * level; the borg types its own keys, so its waits are not */
		if (cap_waits && !net_borg_playing()) {
			net_send_wait(context_name(net_ctx));
		}
		net_flush();
		result = text_get_cmd ? text_get_cmd(context) : 1;
	}
	in_get_cmd = false;
	return result;
}

/**
 * With the text UI: wrap its command hook and claim the prompts the
 * client asked for.  main() calls this once the text UI has started.
 */
void net_game_install(void)
{
	text_get_cmd = cmd_get_hook;
	cmd_get_hook = net_get_cmd;
	monster_swap_hook = net_on_swap;
	panel_change_hook = net_on_panel;
	net_claim_hooks();
	net_screen_hooks(true);
	net_register_events();
#ifdef ALLOW_BORG
	/* The borg reads the map from the game, not the screen, so it can
	 * play with tiles; this stops it turning the terrain back into
	 * letters when it starts */
	borg_graphics = true;
#endif
}

/**
 * ------------------------------------------------------------------------
 * Headless mode
 * ------------------------------------------------------------------------ */

/**
 * The player walked into a store.  Take store commands until the client
 * sends LEAVE_STORE.
 */
static void net_use_store(game_event_type type, game_event_data *data,
		void *user)
{
	if (!store_at(cave, player->grid)) return;
	net_ctx = CTX_STORE;
	leave_store = false;
	while (!leave_store && !net_is_exiting() && !player->is_dead) {
		net_get_cmd(CTX_STORE);
		if (!leave_store) cmdq_pop(CTX_STORE);
	}
	net_ctx = CTX_GAME;
}

/**
 * Create a character from the client's birth commands.
 */
static void net_birth(void)
{
	character_generated = false;
	cmdq_push(CMD_BIRTH_INIT);
	cmdq_execute(CTX_BIRTH);
	/* As the text UI does, start from a reset character */
	cmdq_push(CMD_BIRTH_RESET);
	cmdq_execute(CTX_BIRTH);
	while (!character_generated && !net_is_exiting()) {
		net_get_cmd(CTX_BIRTH);
		cmdq_execute(CTX_BIRTH);
	}
}

/**
 * Play with no text UI.  The client drives everything with commands and
 * answers every prompt.  main() calls this in place of play_game().
 */
void net_game_play(bool new_game)
{
	int i;

	for (i = 0; i < PROMPT_MAX; i++) claimed[i] = true;
	net_claim_hooks();
	cmd_get_hook = net_get_cmd;
	net_register_events();

	init_angband();

	/* A living character in the savefile is played; anything else is born */
	player->is_dead = true;
	if (!new_game && savefile[0] && file_exists(savefile)
			&& !savefile_load(savefile, arg_wizard)) {
		net_exit("the savefile is broken");
	}
	if (player->is_dead || new_game) {
		net_birth();
	} else {
		update_player_object_knowledge(player);
	}

	net_ctx = CTX_GAME;
	event_signal(EVENT_LEAVE_INIT);
	event_signal(EVENT_ENTER_GAME);
	event_signal(EVENT_ENTER_WORLD);
	player->upkeep->autosave = false;
	if (!character_dungeon) prepare_next_level(player);
	on_new_level();

	while (!player->is_dead && player->upkeep->playing) {
		net_get_cmd(CTX_GAME);
		run_game_loop();
	}

	if (player->is_dead) {
		net_ctx = CTX_DEATH;
		event_signal(EVENT_ENTER_DEATH);
		if (cap_snapshots) net_send_snapshot(snapshot_parts);
		net_flush();
		/* Record the death, as close_game() does */
		if (!savefile_save(savefile)) plog("death save failed!");
		net_exit("dead");
	}
	net_save_and_exit("exit");
}

#endif /* USE_NET */
