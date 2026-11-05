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
#include "effects.h"
#include "effects-info.h"
#include "game-event.h"
#include "game-input.h"
#include "game-world.h"
#include "generate.h"
#include "init.h"
#include "main.h"
#include "mon-desc.h"
#include "mon-list.h"
#include "mon-lore.h"
#include "mon-predicate.h"
#include "mon-util.h"
#include "monster.h"
#include "net-frontend.h"
#include "net-glossary.h"
#include "net-json.h"
#include "obj-curse.h"
#include "obj-desc.h"
#include "obj-gear.h"
#include "obj-info.h"
#include "obj-knowledge.h"
#include "obj-list.h"
#include "player-path.h"
#include "obj-pile.h"
#include "obj-tval.h"
#include "obj-util.h"
#include "player.h"
#include "player-birth.h"
#include "grafmode.h"
#include "option.h"
#include "player-calcs.h"
#include "player-history.h"
#include "player-spell.h"
#include "player-timed.h"
#include "player-util.h"
#include "project.h"
#include "savefile.h"
#include "score.h"
#include "store.h"
#include "target.h"
#include "trap.h"
#include "ui-birth.h"
#include "ui-command.h"
#include "ui-context.h"
#include "ui-death.h"
#include "ui-display.h"
#include "ui-equip-cmp.h"
#include "ui-event.h"
#include "ui-game.h"
#include "ui-help.h"
#include "ui-input.h"
#include "ui-knowledge.h"
#include "ui-map.h"
#include "ui-mon-list.h"
#include "ui-mon-lore.h"
#include "ui-object.h"
#include "ui-output.h"
#include "ui-player.h"
#include "ui-prefs.h"
#include "ui-spell.h"
#include "ui-store.h"
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
 * The screens a client can draw itself.
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
	SCREEN_COMMANDS,
	SCREEN_LISTS,
	SCREEN_MAX
};

static const char *screen_names[SCREEN_MAX] = {
	"options", "inventory", "look", "target", "store", "birth", "death",
	"knowledge", "character", "messages", "help", "map", "commands", "lists"
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

/* Drop the frame begun with net_json_begin(), unsent */
static void net_json_cancel(struct net_jw *w)
{
	(void)w;
	net_out.len = json_frame_start;
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

static void net_write_glyph(struct net_jw *w, const struct object *obj);

/**
 * An object's fields, inside an object the caller opened: its reference,
 * its name as object_desc() gives it in mode, its number, weight, and how
 * it looks.
 */
static void net_object_fields_as(struct net_jw *w, const struct object *obj,
		const char *where, int index, uint32_t mode)
{
	char name[160];

	object_desc(name, sizeof(name), obj, mode, player);
	net_write_ref(w, where, index);
	net_jw_kstr(w, "name", name);
	net_jw_kint(w, "number", obj->number);
	net_jw_kint(w, "weight", obj->weight);
	net_write_glyph(w, obj);
}

static void net_object_fields(struct net_jw *w, const struct object *obj,
		const char *where, int index)
{
	net_object_fields_as(w, obj, where, index, ODESC_PREFIX | ODESC_FULL);
}

static void net_write_object(struct net_jw *w, const struct object *obj,
		const char *where, int index)
{
	net_jw_obj(w);
	net_object_fields(w, obj, where, index);
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
		char name[80];

		net_jw_kint(w, "percent", 100L * mon->hp / mon->maxhp);
		my_strcpy(name, mon->race->name, sizeof(name));
		my_strcap(name);
		net_jw_kstr(w, "name", name);
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
 * How an object looks, as fields of the object being written: "char" and
 * "color", and with tiles its "tile".  With tiles the game's letters are
 * tile numbers, and headless mode loads no letters at all, so those use
 * the object's own letter and color from the game data.
 */
static void net_write_glyph(struct net_jw *w, const struct object *obj)
{
	struct object_kind *kind = obj->kind;
	wchar_t c;
	uint8_t a;
	char ch[5];

	if (!kind) return;
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
}

/*
 * As prt_equippy(): the letter for each equipment slot, or null.  With
 * tiles, each entry also has the object's tile.
 */
static void net_write_equippy(struct net_jw *w)
{
	int i;

	net_jw_key(w, "equippy");
	net_jw_arr(w);
	for (i = 0; i < player->body.count; i++) {
		struct object *obj = slot_object(player, i);

		if (!obj || !obj->kind) {
			net_jw_null(w);
			continue;
		}
		net_jw_obj(w);
		net_write_glyph(w, obj);
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

/*
 * Remember which way a creature faces after a step from one grid to the next.
 * A step straight up or down keeps its facing, and a teleport is no step.
 */
static void net_note_facing(int who, struct loc from, struct loc to)
{
	int dx = to.x - from.x, dy = to.y - from.y;

	if (!dx || abs(dx) > 1 || abs(dy) > 1) return;
	if (who < 0) {
		player->upkeep->faces_left = dx < 0;
	} else {
		struct monster *mon = cave_monster(cave, who);

		if (mon) mon->faces_left = dx < 0;
	}
}

static void net_on_swap(struct loc grid1, struct loc grid2, int m1, int m2)
{
	if (m1) net_note_facing(m1, grid1, grid2);
	if (m2) net_note_facing(m2, grid2, grid1);
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

/* The character being made can start as a copy of the last one */
static bool birth_quickstart = false;

/**
 * Keep net_ctx up to date.  Context events go out as EVENT frames too
 * when the client asked for events.
 */
static void net_on_context(game_event_type type, game_event_data *data,
		void *user)
{
	switch (type) {
		case EVENT_ENTER_BIRTH:
			net_ctx = CTX_BIRTH;
			birth_quickstart = data->flag;
			break;
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

/* Point-based stats as the game last reported them, for BIRTH */
static int birth_spent[STAT_MAX];
static int birth_cost[STAT_MAX];
static int birth_left = 0;

static void net_on_birthpoints(game_event_type type, game_event_data *data,
		void *user)
{
	int i;

	for (i = 0; i < STAT_MAX; i++) {
		birth_spent[i] = data->birthpoints.points[i];
		birth_cost[i] = data->birthpoints.inc_points[i];
	}
	birth_left = data->birthpoints.remaining;
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
	event_remove_handler(EVENT_BIRTHPOINTS, net_on_birthpoints, NULL);
	event_add_handler(EVENT_BIRTHPOINTS, net_on_birthpoints, NULL);
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
	/* As the text UI's prompts do, so no message waits behind one */
	if (!net_headless) event_signal(EVENT_MESSAGE_FLUSH);
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
 * Whether a keymap has keys still to type, which answer the prompt the
 * game is about to ask.  The text UI's own prompt then reads them, as it
 * would without the client, so a keymap such as "quaff the first potion"
 * still works.
 */
static bool net_keys_queued(void)
{
	return !net_headless && text_hooks.saved && inkey_next
		&& inkey_next->code;
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

	if (net_keys_queued()) return text_hooks.get_check(prompt);

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

	if (net_keys_queued()) return text_hooks.get_string(prompt, buf, len);

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

	if (net_keys_queued()) return text_hooks.get_quantity(prompt, max);

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

	if (net_keys_queued()) {
		return text_hooks.get_char(prompt, options, len, fallback);
	}

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

	if (net_keys_queued()) return text_hooks.get_com(prompt, command);

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
	if (net_keys_queued()) return text_hooks.get_rep_dir(dir, allow_none);
	return net_ask_dir(PROMPT_DIRECTION, dir, allow_none, false);
}

/**
 * The aiming prompt.  The client may set a target with SET_TARGET while
 * it is open and answer 5, for the target.  As textui_get_aim_dir() does,
 * the old target is used without asking when use_old_target is on, and 5
 * needs a target.
 */
static bool net_get_aim_dir(int *dir)
{
	if (net_keys_queued()) return text_hooks.get_aim_dir(dir);
	*dir = 0;
	if (OPT(player, use_old_target) && target_okay()) {
		*dir = DIR_TARGET;
		return true;
	}
	if (!net_ask_dir(PROMPT_AIM, dir, false, true)) return false;
	return *dir != DIR_TARGET || target_okay();
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

/**
 * One spell, as the game's spell menu shows it (spell_menu_display() in
 * ui-spell.c, which this must be kept in step with): its level, mana,
 * fail rate, and a comment such as "untried" or its damage, in the color
 * the menu gives the row.  "usable" says the prompt takes it.
 */
static void net_write_spell(struct net_jw *w, int sidx, bool usable)
{
	const struct class_spell *spell = spell_by_index(player, sidx);
	char info[30] = "";
	const char *comment = "";
	uint8_t attr;

	if (!spell) return;
	if (spell->slevel >= 99) {
		comment = "illegible";
		attr = COLOUR_L_DARK;
	} else if (player->spell_flags[sidx] & PY_SPELL_FORGOTTEN) {
		comment = "forgotten";
		attr = COLOUR_YELLOW;
	} else if (player->spell_flags[sidx] & PY_SPELL_LEARNED) {
		if (player->spell_flags[sidx] & PY_SPELL_WORKED) {
			get_spell_info(sidx, info, sizeof(info));
			comment = info[0] == ' ' ? info + 1 : info;
			attr = COLOUR_WHITE;
		} else {
			comment = "untried";
			attr = COLOUR_L_GREEN;
		}
	} else if (spell->slevel <= player->lev) {
		comment = "unknown";
		attr = COLOUR_L_BLUE;
	} else {
		comment = "difficult";
		attr = COLOUR_RED;
	}

	net_jw_obj(w);
	net_jw_kint(w, "value", sidx);
	net_jw_kstr(w, "name", spell->name);
	net_jw_kint(w, "level", spell->slevel);
	net_jw_kint(w, "mana", spell->smana);
	net_jw_kint(w, "fail", spell_chance(sidx));
	net_jw_kstr(w, "comment", comment);
	net_jw_kint(w, "color", attr);
	net_jw_kbool(w, "usable", usable);
	net_jw_obj_end(w);
}

/**
 * What the book's realm calls a spell, such as "prayer".
 */
static const char *net_spell_noun(const struct object *book)
{
	const struct class_book *b = book ?
		player_object_to_book(player, book) : NULL;

	return b && b->realm ? b->realm->spell_noun : "spell";
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
	int i, n, ok = 0;
	char name[160];

	if (net_keys_queued()) {
		return text_hooks.get_spell_from_book(p, verb, book, error,
			spell_filter);
	}
	object_desc(name, sizeof(name), book, ODESC_PREFIX | ODESC_FULL, p);
	net_request_begin(&w, PROMPT_SPELL_FROM_BOOK, NULL);
	net_jw_kstr(&w, "verb", verb);
	net_jw_kstr(&w, "noun", net_spell_noun(book));
	net_jw_kstr(&w, "book", name);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	n = spell_collect_from_book(p, book, &spells);
	for (i = 0; i < n; i++) {
		bool usable = !spell_filter || spell_filter(p, spells[i]);

		net_write_spell(&w, spells[i], usable);
		if (usable) ok++;
	}
	mem_free(spells);
	net_jw_arr_end(&w);
	if (ok == 0 && !net_headless) {
		/* Nothing to choose: say so, as the text UI's spell menu does */
		net_json_cancel(&w);
		if (error) msg("%s", error);
		return -1;
	}
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
	int i, j, n, sidx = -1, count = 0;
	const char *noun = NULL;

	if (net_keys_queued()) {
		return text_hooks.get_spell(p, verb, book_filter, cmd, book_error,
			spell_filter, spell_error, rtn_book);
	}
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
		if (!noun) noun = net_spell_noun(book);
		n = spell_collect_from_book(p, book, &spells);
		for (j = 0; j < n; j++) {
			if (spell_filter && !spell_filter(p, spells[j])) continue;
			net_write_spell(&w, spells[j], true);
			count++;
		}
		mem_free(spells);
	}
	net_jw_arr_end(&w);
	if (count == 0 && !net_headless) {
		/* Nothing to choose: say so, as the text UI does */
		net_json_cancel(&w);
		if (spell_error) msg("%s", spell_error);
		return -1;
	}
	net_jw_kstr(&w, "noun", noun ? noun : "spell");
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

/**
 * Browse a book, as textui_spell_browse() does, with the book's spells
 * sent as a spell_from_book prompt with the verb "browse".  The client
 * shows them and closes the prompt; nothing is cast.
 */
static void net_spell_browse(void)
{
	struct object *obj;

	if (!get_item(&obj, "Browse which book? ",
			"You have no books that you can read.",
			CMD_BROWSE_SPELL, obj_can_browse,
			(USE_INVEN | USE_FLOOR | IS_HARMLESS))) {
		return;
	}
	track_object(player->upkeep, obj);
	handle_stuff(player);
	if (!player_object_to_book(player, obj)) {
		msg("You cannot browse that.");
		return;
	}
	(void)net_get_spell_from_book(player, "browse", obj, NULL,
		spell_okay_to_browse);
}

/**
 * The digits that pick obj in the game's item menu for cmd: its "@n"
 * inscriptions, and "@xn" where x is the command's key, as get_tag() in
 * ui-object.c reads them.
 */
static void net_write_tags(struct net_jw *w, const struct object *obj,
		cmd_code cmd)
{
	char tags[16] = "";
	size_t n = 0;
	const char *s;
	int mode = OPT(player, rogue_like_commands) ?
		KEYMAP_MODE_ROGUE : KEYMAP_MODE_ORIG;
	unsigned char cmdkey = cmd_lookup_key_unktrl(cmd, mode);

	if (!obj->note) return;
	for (s = strchr(quark_str(obj->note), '@'); s && n + 1 < sizeof(tags);
			s = strchr(s + 1, '@')) {
		char tag = 0;

		if (isdigit((unsigned char)s[1])) {
			tag = s[1];
		} else if (cmdkey && s[1] == cmdkey
				&& isdigit((unsigned char)s[2])) {
			tag = s[2];
		}
		if (tag && !strchr(tags, tag)) {
			tags[n++] = tag;
			tags[n] = 0;
		}
	}
	if (n) net_jw_kstr(w, "tags", tags);
}

static int net_item_choice(struct net_jw *w, struct object *obj,
		item_tester tester, cmd_code cmd, const char *where, int index)
{
	if (!obj || (tester && !tester(obj))) return 0;
	net_jw_obj(w);
	net_object_fields(w, obj, where, index);
	net_write_tags(w, obj, cmd);
	net_jw_obj_end(w);
	return 1;
}

static bool net_get_item(struct object **choice, const char *pmt,
		const char *str, cmd_code cmd, item_tester tester, int mode)
{
	struct net_jw w;
	struct net_json *r;
	struct object *obj = NULL;
	int i, n = 0;
	int keymap = OPT(player, rogue_like_commands) ?
		KEYMAP_MODE_ROGUE : KEYMAP_MODE_ORIG;

	/* Keys in the queue answer the classic prompt. So do the game's own
	 * pack and equipment listings, which ask with SHOW_EMPTY: with the
	 * native inventory off, they stay the game's. */
	if (net_keys_queued()
			|| (!screen_claimed[SCREEN_INVENTORY] && (mode & SHOW_EMPTY))) {
		return text_hooks.get_item(choice, pmt, str, cmd, tester, mode);
	}
	net_request_begin(&w, PROMPT_ITEM, pmt);
	net_jw_key(&w, "choices");
	net_jw_arr(&w);
	if (mode & USE_INVEN) {
		for (i = 0; i < z_info->pack_size; i++) {
			n += net_item_choice(&w, player->upkeep->inven[i], tester, cmd,
				"inven", i);
		}
	}
	if (mode & USE_EQUIP) {
		for (i = 0; i < player->body.count; i++) {
			n += net_item_choice(&w, slot_object(player, i), tester, cmd,
				"equip", i);
		}
	}
	if (mode & USE_QUIVER) {
		for (i = 0; i < z_info->quiver_size; i++) {
			n += net_item_choice(&w, player->upkeep->quiver[i], tester, cmd,
				"quiver", i);
		}
	}
	if ((mode & USE_FLOOR) && cave) {
		struct object **floor = mem_zalloc(z_info->floor_size
			* sizeof(*floor));
		int nf = net_floor_list(floor, z_info->floor_size);

		for (i = 0; i < nf; i++) {
			n += net_item_choice(&w, floor[i], tester, cmd, "floor", i);
		}
		mem_free(floor);
	}
	net_jw_arr_end(&w);
	if (n == 0 && !net_headless) {
		/* Nothing fits: say so, as the text UI's item menu does */
		net_json_cancel(&w);
		if (str) msg("%s", str);
		return false;
	}
	r = net_request_wait(&w);
	obj = r ? net_resolve_item(net_json_get(r, "item")) : NULL;
	net_json_free(r);
	if (!obj || (tester && !tester(obj))) return false;
	/* Inscriptions such as !d ask before the command goes ahead */
	if (!net_headless && !get_item_allow(obj,
			cmd_lookup_key_unktrl(cmd, keymap), cmd, mode & IS_HARMLESS)) {
		return false;
	}
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
 * Point the text UI's command to browse a book at net_spell_browse(), or
 * back at its own, which draws the book's spells itself.
 */
static void net_browse_hook(bool claim)
{
	const struct command_list *l;
	size_t i;

	for (l = cmds_all; l->name; l++) {
		for (i = 0; i < l->len; i++) {
			struct cmd_info *c = &l->list[i];

			if (claim && c->hook == textui_spell_browse) {
				c->hook = net_spell_browse;
			} else if (!claim && c->hook == net_spell_browse) {
				c->hook = textui_spell_browse;
			}
		}
	}
}

/**
 * Give the text UI back its input hooks.
 */
static void net_release_hooks(void)
{
	if (!text_hooks.saved) return;
	net_browse_hook(false);
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
	net_browse_hook(claimed[PROMPT_SPELL_FROM_BOOK] && !net_headless);
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

/* The text UI has yet to wake for the command pushed */
static bool wake_pending = false;

/* The player asked to leave the store (headless mode) */
static bool leaving_store = false;

/* A screen's REQUEST is waiting for its REPLY */
static bool screen_open = false;

/* The request of the open store or birth screen, which takes that
 * context's commands, or 0 */
static int session_id = 0;

/* The command the open store or birth screen sent, whose result goes out
 * once it has run */
static cmd_code session_code = CMD_NULL;
static int session_cmd_id = 0;
static bool session_have_id = false;

/* The COMMAND just handled sends its result later */
static bool result_deferred = false;

/* The stats came from the roller, so points cannot buy them */
static bool birth_rolled = false;

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

/**
 * The text of a spell's description, as the game's spell menu shows it
 * (spell_menu_browser() in ui-spell.c, which this must be kept in step
 * with): the spell's own text, then its average damage once it has
 * worked.
 */
static void net_spell_text(textblock *tb, int sidx)
{
	const struct class_spell *spell = spell_by_index(player, sidx);
	struct effect *e;
	dice_t *shared_dice = NULL;
	int num_damaging = 0, i = 0;

	textblock_append(tb, "%s", spell->text);
	for (e = spell->effect; e; e = effect_next(e)) {
		if (effect_damages(e)) num_damaging++;
	}
	if (num_damaging == 0
			|| !(player->spell_flags[sidx] & PY_SPELL_WORKED)
			|| (player->spell_flags[sidx] & PY_SPELL_FORGOTTEN)) {
		return;
	}
	textblock_append(tb, "  Inflicts an average of");
	for (e = spell->effect; e; e = effect_next(e)) {
		const char *projection;

		if (e->index == EF_SET_VALUE) {
			shared_dice = e->dice;
		} else if (e->index == EF_CLEAR_VALUE) {
			shared_dice = NULL;
		}
		if (!effect_damages(e)) continue;
		if (num_damaging > 2 && i > 0) textblock_append(tb, ",");
		if (num_damaging > 1 && i == num_damaging - 1) {
			textblock_append(tb, " and");
		}
		textblock_append_c(tb, COLOUR_L_GREEN, " %d",
			effect_avg_damage(e, shared_dice));
		projection = effect_projection(e);
		if (strlen(projection) > 0) textblock_append(tb, " %s", projection);
		i++;
	}
	textblock_append(tb, " damage.");
}

/**
 * The object {"grid": {"x": X, "y": Y}, "object": n} names: the nth object
 * the player knows of on that grid, in the order LOOK lists them, or NULL.
 */
static struct object *net_grid_object(const struct net_json *at,
		const struct net_json *args)
{
	struct object **list, *obj = NULL;
	int x, y, i = 0, n;

	if (!at || !cave || !player || !player->cave
			|| !net_json_get_int(at, "x", &x)
			|| !net_json_get_int(at, "y", &y)
			|| !square_in_bounds_fully(cave, loc(x, y))) {
		return NULL;
	}
	net_json_get_int(args, "object", &i);
	list = mem_zalloc(z_info->floor_size * sizeof(*list));
	n = scan_distant_floor(list, z_info->floor_size, player, loc(x, y));
	if (i >= 0 && i < n) obj = cave->objects[list[i]->oidx];
	mem_free(list);
	return obj;
}

static bool know_section(const char *name, enum knowledge_section *out);
static int know_store(const char *name);

/* The nth object of a store's stock, as KNOWLEDGE lists it */
static struct object *net_store_object(int store, int n)
{
	struct object **list = mem_zalloc(z_info->store_inven_max * sizeof(*list));
	struct object *obj = NULL;

	store_stock_list(&stores[store], list, z_info->store_inven_max);
	if (n >= 0 && n < z_info->store_inven_max
			&& n < stores[store].stock_num) {
		obj = list[n];
	}
	mem_free(list);
	return obj;
}

/**
 * RECALL describes an object, a spell, a monster, a trap, or terrain as
 * the game's own screens do, as styled text: {"item": REF}, {"spell": n}
 * with a spell's value from a spell prompt or SPELLS, {"monster": n} with
 * a monster race's id from VISUALS or LOOK, {"trap": n} or {"feature": n}
 * with a trap kind's or a terrain feature's from LOOK, or {"grid": {"x":
 * X, "y": Y}, "object": n} for the nth object LOOK lists on that grid.
 * {"section": NAME, "id": n} describes an entry KNOWLEDGE lists, as the
 * knowledge menu does.  The answer has "title" and "description", which
 * are null when there is nothing to describe.
 */
static void net_write_entry_glyph(struct net_jw *w,
		const struct knowledge_entry *e);

static void net_query_recall(const struct net_json *v)
{
	const struct monster_race *recalled = NULL;
	const struct net_json *args = net_json_get(v, "args");
	struct net_jw w;
	textblock *tb = NULL;
	char title[160] = "";
	int n;

	if (player && player->upkeep && args) {
		struct object *obj = net_json_get(args, "item") ?
			net_resolve_item(net_json_get(args, "item")) : NULL;
		const char *section = net_json_get_str(args, "section");
		enum knowledge_section know;

		if (section) {
			if (!net_json_get_int(args, "id", &n)) {
				/* Nothing to describe */
			} else if (know_section(section, &know)) {
				tb = knowledge_lore(know, n, title, sizeof(title));
			} else if (know_store(section) >= 0
					&& (obj = net_store_object(know_store(section), n))) {
				tb = object_info(obj, OINFO_NONE);
				object_desc(title, sizeof(title), obj,
					ODESC_PREFIX | ODESC_FULL | ODESC_CAPITAL, player);
			}
		} else if (obj) {
			tb = object_info(obj, OINFO_NONE);
			object_desc(title, sizeof(title), obj,
				ODESC_PREFIX | ODESC_FULL | ODESC_CAPITAL, player);
		} else if (net_json_get(args, "item")
				&& net_json_get_int(net_json_get(args, "item"), "compare", &n)
				&& character_generated && equip_cmp_item(n)) {
			/* An item COMPARE lists, wherever it is */
			const struct object *cobj = equip_cmp_item(n);

			tb = object_info(cobj, OINFO_NONE);
			object_desc(title, sizeof(title), cobj,
				ODESC_PREFIX | ODESC_FULL | ODESC_CAPITAL, player);
		} else if (net_json_get_int(args, "spell", &n) && player->class
				&& spell_by_index(player, n)) {
			tb = textblock_new();
			net_spell_text(tb, n);
			my_strcpy(title, spell_by_index(player, n)->name, sizeof(title));
		} else if (net_json_get_int(args, "monster", &n) && n > 0
				&& n < z_info->r_max && r_info[n].name) {
			const struct monster_race *race = &r_info[n];

			tb = textblock_new();
			lore_description_headed(tb, race, get_lore(race));
			my_strcpy(title, race->name, sizeof(title));
			recalled = race;
		} else if (net_json_get_int(args, "trap", &n) && n >= 0
				&& n < z_info->trap_max && trap_info[n].desc) {
			/* As trap_lore() in ui-knowledge.c */
			tb = textblock_new();
			if (trap_info[n].text) {
				textblock_append(tb, "%s", trap_info[n].text);
			}
			my_strcpy(title, trap_info[n].desc, sizeof(title));
			my_strcap(title);
		} else if (net_json_get_int(args, "feature", &n) && n >= 0
				&& n < FEAT_MAX && f_info[n].name) {
			/* As feat_lore() in ui-knowledge.c */
			tb = textblock_new();
			if (f_info[n].desc) textblock_append(tb, "%s", f_info[n].desc);
			my_strcpy(title, f_info[n].name, sizeof(title));
			my_strcap(title);
		} else if ((obj = net_grid_object(net_json_get(args, "grid"),
				args))) {
			/* As look mode's recall of an object */
			tb = object_info(obj, OINFO_NONE);
			object_desc(title, sizeof(title), obj,
				ODESC_PREFIX | ODESC_FULL | ODESC_CAPITAL, player);
		}
	}

	net_answer_begin(&w, "RECALL", v);
	if (tb) {
		net_jw_kstr(&w, "title", title);
		net_jw_key(&w, "description");
		net_jw_textblock(&w, tb);
		textblock_free(tb);
		if (recalled) {
			struct knowledge_entry e;

			memset(&e, 0, sizeof(e));
			e.glyph = true;
			e.attr = monster_x_attr[recalled->ridx];
			e.ch = monster_x_char[recalled->ridx];
			e.d_attr = recalled->d_attr;
			e.d_char = recalled->d_char;
			net_jw_key(&w, "glyph");
			net_jw_obj(&w);
			net_write_entry_glyph(&w, &e);
			net_jw_obj_end(&w);
		}
	} else {
		net_jw_key(&w, "title");
		net_jw_null(&w);
		net_jw_key(&w, "description");
		net_jw_null(&w);
	}
	net_json_end(&w);
}

/**
 * ------------------------------------------------------------------------
 * Looking and targeting
 * ------------------------------------------------------------------------ */

/*
 * How a grid looks on the map, as fields of the object being written:
 * "char" and "color", and with tiles its "tile".  With tiles the map's
 * letters are tile numbers, so "char" and "color" are then the thing's
 * own, which the caller gives.
 */
static void net_write_grid_glyph(struct net_jw *w, struct loc grid,
		wchar_t own_char, uint8_t own_attr)
{
	struct grid_data g;
	int a, ta;
	wchar_t c, tc;
	char ch[5];

	if (net_headless) {
		net_utf8(own_char, ch);
		net_jw_kstr(w, "char", ch);
		net_jw_kint(w, "color", own_attr);
		return;
	}
	map_info(grid, &g);
	grid_data_as_text(&g, &a, &c, &ta, &tc);
	if (use_graphics == GRAPHICS_NONE) {
		net_utf8(c, ch);
		net_jw_kstr(w, "char", ch);
		net_jw_kint(w, "color", a);
		return;
	}
	net_utf8(own_char, ch);
	net_jw_kstr(w, "char", ch);
	net_jw_kint(w, "color", own_attr);
	net_jw_key(w, "tile");
	net_jw_obj(w);
	net_jw_kint(w, "row", a & 0x7F);
	net_jw_kint(w, "col", c & 0x7F);
	net_jw_obj_end(w);
}

/**
 * One grid of LOOK, as look mode describes it (target_set_interactive_aux()
 * and its handlers in ui-target.c, which this must be kept in step with):
 * the first thing look mode shows there, which is a monster, a trap, an
 * object or a pile of them, or the terrain, and the sentence it prints,
 * such as "You see a Jackal (unhurt, asleep), 3 N, 2 E.".  Everything
 * else the player knows to be there comes too.  "free" marks a grid the
 * player picked that look mode would pass over.
 */
static void net_write_look(struct net_jw *w, struct loc grid, bool free)
{
	struct chunk *c = cave;
	struct player *p = player;
	struct monster *mon = square_monster(c, grid);
	struct trap *trap = square_isvisibletrap(p->cave, grid) ?
		square(p->cave, grid)->trap : NULL;
	int floor_max = z_info->floor_size;
	struct object **floor_list = mem_zalloc(floor_max * sizeof(*floor_list));
	int floor_num = scan_distant_floor(floor_list, floor_max, p, grid);
	int feat = square(p->cave, grid)->feat;
	const char *phrase1, *phrase2;
	const char *kind;
	char where[50], name[120], text[TARGET_OUT_VAL_SIZE];
	wchar_t own_char = f_info[feat].d_char;
	uint8_t own_attr = f_info[feat].d_attr;
	int col, row, i;

	if (mon && !monster_is_obvious(mon)) mon = NULL;
	coords_desc(where, sizeof(where), grid.y, grid.x);

	/* As aux_reinit() */
	if (square(c, grid)->mon < 0) {
		phrase1 = "You are ";
		phrase2 = "on ";
	} else {
		phrase1 = square_isseen(c, grid) ? "You see " :
			(mon ? "You sense " : "You recall ");
		phrase2 = "";
	}

	net_jw_obj(w);
	net_jw_kint(w, "x", grid.x);
	net_jw_kint(w, "y", grid.y);
	if (!net_headless && net_grid_cell(grid, &col, &row)) {
		net_jw_key(w, "cell");
		net_jw_arr(w);
		net_jw_int(w, col);
		net_jw_int(w, row);
		net_jw_arr_end(w);
	}
	if (free) net_jw_kbool(w, "free", true);
	net_jw_kstr(w, "where", where);

	if (p->timed[TMD_IMAGE]) {
		/* As aux_hallucinate() */
		kind = "strange";
		my_strcpy(name, "something strange", sizeof(name));
		strnfmt(text, sizeof(text), "%s%s%s, %s.", phrase1, phrase2,
			name, where);
	} else if (mon && square(c, grid)->mon > 0) {
		/* As aux_monster() */
		char health[80] = "";

		kind = "monster";
		monster_desc(name, sizeof(name), mon, MDESC_IND_VIS);
		look_mon_desc(health, sizeof(health), mon->midx);
		strnfmt(text, sizeof(text), "%s%s%s (%s), %s.", phrase1, phrase2,
			name, health, where);
		own_char = mon->race->d_char;
		own_attr = mon->race->d_attr;
		net_jw_kint(w, "monster", mon->race->ridx);
		net_jw_kint(w, "midx", mon->midx);
		net_jw_kstr(w, "health", health);
		net_jw_kbool(w, "targetable", target_able(mon));
	} else if (trap) {
		/* As aux_trap() */
		kind = "trap";
		strnfmt(name, sizeof(name), "%s%s",
			is_a_vowel(trap->kind->desc[0]) ? "an " : "a ",
			trap->kind->desc);
		strnfmt(text, sizeof(text), "%s%s%s, %s.", phrase1, phrase2,
			name, where);
		own_char = trap->kind->d_char;
		own_attr = trap->kind->d_attr;
	} else if (floor_num > 1) {
		/* As aux_object() */
		kind = "pile";
		strnfmt(name, sizeof(name), "a pile of %d objects", floor_num);
		strnfmt(text, sizeof(text), "%s%s%s, %s.", phrase1, phrase2,
			name, where);
		own_char = floor_list[0]->kind->d_char;
		own_attr = floor_list[0]->kind->d_attr;
	} else if (floor_num == 1) {
		/* As target_recall_loop_object() */
		kind = "object";
		object_desc(name, sizeof(name), cave->objects[floor_list[0]->oidx],
			ODESC_PREFIX | ODESC_FULL, p);
		strnfmt(text, sizeof(text), "%s%s%s, %s.", phrase1, phrase2,
			name, where);
		own_char = floor_list[0]->kind->d_char;
		own_attr = floor_list[0]->kind->d_attr;
	} else {
		/* As aux_terrain() */
		const char *prep = *phrase2 ?
			square_apparent_look_in_preposition(p->cave, grid) : "";

		kind = (square(c, grid)->mon < 0) ? "player" : "terrain";
		strnfmt(name, sizeof(name), "%s%s",
			square_apparent_look_prefix(p->cave, grid),
			square_apparent_name(p->cave, grid));
		strnfmt(text, sizeof(text), "%s%s%s, %s.", phrase1, prep, name,
			where);
	}
	if (square(c, grid)->mon < 0) {
		kind = "player";
		own_char = L'@';
		own_attr = COLOUR_WHITE;
	}
	net_jw_kstr(w, "kind", kind);
	net_jw_kstr(w, "name", name);
	net_jw_kstr(w, "text", text);
	net_write_grid_glyph(w, grid, own_char, own_attr);

	if (!p->timed[TMD_IMAGE]) {
		if (trap) net_jw_kint(w, "trap", trap->kind->tidx);
		if (square_isknown(c, grid)) net_jw_kint(w, "feature", feat);
		if (floor_num > 0) {
			net_jw_key(w, "objects");
			net_jw_arr(w);
			for (i = 0; i < floor_num; i++) {
				struct object *obj = cave->objects[floor_list[i]->oidx];
				char o_name[120];

				object_desc(o_name, sizeof(o_name), obj,
					ODESC_PREFIX | ODESC_FULL, p);
				net_jw_obj(w);
				net_jw_kstr(w, "name", o_name);
				net_write_glyph(w, obj);
				net_jw_obj_end(w);
			}
			net_jw_arr_end(w);
		}
	}
	net_jw_obj_end(w);
	mem_free(floor_list);
}

/**
 * LOOK {"mode": "look"} lists the interesting grids look mode would step
 * through, nearest first, as target_get_monsters() finds them: with the
 * main term, only those the map shows.  {"mode": "target"} lists only the
 * monsters that can be targeted.  {"x": X, "y": Y} adds that grid, first,
 * if it is not in the list, as look mode does for a grid the player
 * clicks.  The answer also has "target", the grid the player targets, if
 * any, and with "monster" the monster's index.
 */
static void net_query_look(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *mode_name = args ? net_json_get_str(args, "mode") : NULL;
	int mode = (mode_name && streq(mode_name, "target")) ?
		TARGET_KILL : TARGET_LOOK;
	struct net_jw w;
	struct point_set *targets;
	struct loc pick = loc(-1, -1);
	term *old = Term;
	int x, y, i;

	net_answer_begin(&w, "LOOK", v);
	if (!character_dungeon || !cave || !player || !player->cave) {
		net_jw_key(&w, "things");
		net_jw_arr(&w);
		net_jw_arr_end(&w);
		net_json_end(&w);
		return;
	}
	if (args && net_json_get_int(args, "x", &x)
			&& net_json_get_int(args, "y", &y)
			&& square_in_bounds_fully(cave, loc(x, y))) {
		pick = loc(x, y);
	}

	/* The panel is the main term's */
	if (!net_headless && term_screen) Term_activate(term_screen);
	targets = target_get_monsters(mode, NULL,
		!net_headless && term_screen != NULL);
	net_jw_key(&w, "things");
	net_jw_arr(&w);
	if (pick.x >= 0) {
		bool listed = false;

		for (i = 0; i < point_set_size(targets); i++) {
			if (loc_eq(targets->pts[i], pick)) listed = true;
		}
		if (!listed) net_write_look(&w, pick, true);
	}
	for (i = 0; i < point_set_size(targets); i++) {
		net_write_look(&w, targets->pts[i], false);
	}
	net_jw_arr_end(&w);
	point_set_dispose(targets);
	if (!net_headless) Term_activate(old);

	if (target_okay()) {
		struct loc grid;
		struct monster *mon = target_get_monster();

		target_get(&grid);
		net_jw_key(&w, "target");
		net_jw_obj(&w);
		net_jw_kint(&w, "x", grid.x);
		net_jw_kint(&w, "y", grid.y);
		if (mon) net_jw_kint(&w, "monster", mon->midx);
		net_jw_obj_end(&w);
	}
	net_json_end(&w);
}

/**
 * MONSTER_LIST names the monster on each row of the monster list
 * subwindow, as it shows now: "rows" has a monster race's id, as RECALL
 * takes it, or null, for each row.  It is empty when no subwindow shows
 * the monster list.
 */
static void net_query_monster_list(const struct net_json *v)
{
	struct net_jw w;
	const struct monster_race *races[64];
	int i, n = 0;

	if (character_dungeon && !net_headless) {
		for (i = 0; i < ANGBAND_TERM_MAX; i++) {
			term *t = angband_term[i];

			if (!t || !(window_flag[i] & PW_MONLIST)) continue;
			n = monster_list_subwindow_races(t->hgt, t->wid, races,
				(int)N_ELEMENTS(races));
			break;
		}
	}
	net_answer_begin(&w, "MONSTER_LIST", v);
	net_jw_key(&w, "rows");
	net_jw_arr(&w);
	for (i = 0; i < n; i++) {
		if (races[i]) {
			net_jw_int(&w, races[i]->ridx);
		} else {
			net_jw_null(&w);
		}
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
}

static const char *net_cmd_name(cmd_code code);

/**
 * One thing the game's menu for an object (context_menu_object() in
 * ui-context.c) offers to do with it: the command, the key the menu gives
 * it in the player's keyset, and whether it can be done now.  "arg" names
 * the command's argument for the object when it is not "item", and
 * "quantity", when there, is a quantity to send with it.
 */
static void net_action(struct net_jw *w, const char *label, cmd_code cmd,
		bool valid, const char *arg, int quantity)
{
	bool rogue = OPT(player, rogue_like_commands);
	unsigned char key = cmd_lookup_key(cmd,
		rogue ? KEYMAP_MODE_ROGUE : KEYMAP_MODE_ORIG);
	char k[2] = { 0, 0 };

	/* Drop all has a key of its own, which the roguelike keys move */
	if (quantity > 0) key = rogue ? 'A' : 'D';
	if (key >= ' ' && key < 0x7F) k[0] = (char)key;

	net_jw_obj(w);
	net_jw_kstr(w, "label", label);
	net_jw_kstr(w, "cmd", net_cmd_name(cmd));
	if (k[0]) net_jw_kstr(w, "key", k);
	net_jw_kbool(w, "valid", valid);
	if (arg) net_jw_kstr(w, "arg", arg);
	if (quantity > 0) net_jw_kint(w, "quantity", quantity);
	net_jw_obj_end(w);
}

/**
 * What can be done with a carried object, as context_menu_object() works
 * it out, which this must be kept in step with.  Inspect is left out,
 * since RECALL gives the description, and so is Ignore, which opens a
 * menu of its own.  BROWSE_SPELL is for the client to show the book's
 * spells, with SPELLS; it is not a game command.
 */
static void net_write_actions(struct net_jw *w, const struct object *obj)
{
	bool equipped = object_is_equipped(player->body, obj);

	net_jw_key(w, "actions");
	net_jw_arr(w);
	if (obj_can_browse(obj)) {
		if (obj_can_cast_from(obj) && player_can_cast(player, false)) {
			net_action(w, "Cast", CMD_CAST, true, "book", 0);
		}
		if (obj_can_study(obj) && player_can_study(player, false)) {
			net_action(w, "Study", CMD_STUDY, true, NULL, 0);
		}
		if (player_can_read(player, false)) {
			net_action(w, "Browse", CMD_BROWSE_SPELL, true, NULL, 0);
		}
	} else if (obj_is_useable(obj)) {
		if (tval_is_wand(obj)) {
			net_action(w, "Aim", CMD_USE_WAND, obj_has_charges(obj), NULL, 0);
		} else if (tval_is_rod(obj)) {
			net_action(w, "Zap", CMD_USE_ROD, obj_can_zap(obj), NULL, 0);
		} else if (tval_is_staff(obj)) {
			net_action(w, "Use", CMD_USE_STAFF, obj_has_charges(obj), NULL, 0);
		} else if (tval_is_scroll(obj)) {
			net_action(w, "Read", CMD_READ_SCROLL,
				player_can_read(player, false), NULL, 0);
		} else if (tval_is_potion(obj)) {
			net_action(w, "Quaff", CMD_QUAFF, true, NULL, 0);
		} else if (tval_is_edible(obj)) {
			net_action(w, "Eat", CMD_EAT, true, NULL, 0);
		} else if (obj_is_activatable(obj)) {
			net_action(w, "Activate", CMD_ACTIVATE,
				equipped && obj_can_activate(obj), NULL, 0);
		} else if (obj_can_fire(obj)) {
			net_action(w, "Fire", CMD_FIRE, true, NULL, 0);
		} else {
			net_action(w, "Use", CMD_USE, true, NULL, 0);
		}
	}
	if (obj_can_refill(obj)) net_action(w, "Refill", CMD_REFILL, true, NULL, 0);
	if (equipped && obj_can_takeoff(obj)) {
		net_action(w, "Take off", CMD_TAKEOFF, true, NULL, 0);
	} else if (!equipped && obj_can_wear(obj)) {
		net_action(w, "Equip", CMD_WIELD, true, NULL, 0);
	}
	if (object_is_carried(player, obj) && cave
			&& !square_isshop(cave, player->grid)) {
		net_action(w, "Drop", CMD_DROP, true, NULL, 0);
		if (obj->number > 1) {
			net_action(w, "Drop all", CMD_DROP, true, NULL, obj->number);
		}
	}
	if (obj_can_throw(obj)) net_action(w, "Throw", CMD_THROW, true, NULL, 0);
	net_action(w, "Inscribe", CMD_INSCRIBE, true, NULL, 0);
	if (obj_has_inscrip(obj)) {
		net_action(w, "Uninscribe", CMD_UNINSCRIBE, true, NULL, 0);
	}
	net_jw_arr_end(w);
}

static void net_write_carried(struct net_jw *w, const struct object *obj,
		const char *where, int index)
{
	net_jw_obj(w);
	net_object_fields(w, obj, where, index);
	net_write_actions(w, obj);
	net_jw_obj_end(w);
}

/**
 * INVENTORY lists what the character carries, for the inventory screen:
 * the pack, the equipment, and the quiver, each object with the actions
 * the game's own menu for it offers.  "slots" names every equipment
 * slot, so a client can show the empty ones; "burden" is the weight
 * carried and "remaining" what is left before it slows the character,
 * both in tenths of a pound.
 */
static void net_query_inventory(const struct net_json *v)
{
	struct net_jw w;
	int i;

	net_answer_begin(&w, "INVENTORY", v);
	if (player && player->upkeep && player->upkeep->inven) {
		net_jw_key(&w, "inventory");
		net_jw_arr(&w);
		for (i = 0; i < z_info->pack_size; i++) {
			struct object *obj = player->upkeep->inven[i];

			if (obj) net_write_carried(&w, obj, "inven", i);
		}
		net_jw_arr_end(&w);

		net_jw_key(&w, "slots");
		net_jw_arr(&w);
		for (i = 0; i < player->body.count; i++) {
			net_jw_str(&w, player->body.slots[i].name);
		}
		net_jw_arr_end(&w);

		net_jw_key(&w, "equipment");
		net_jw_arr(&w);
		for (i = 0; i < player->body.count; i++) {
			struct object *obj = slot_object(player, i);

			if (obj) net_write_carried(&w, obj, "equip", i);
		}
		net_jw_arr_end(&w);

		net_jw_key(&w, "quiver");
		net_jw_arr(&w);
		for (i = 0; i < z_info->quiver_size; i++) {
			struct object *obj = player->upkeep->quiver[i];

			if (obj) net_write_carried(&w, obj, "quiver", i);
		}
		net_jw_arr_end(&w);

		net_jw_kint(&w, "burden", player->upkeep->total_weight);
		net_jw_kint(&w, "remaining", weight_remaining(player));
	}
	net_json_end(&w);
}

/**
 * SPELLS lists the spells in a book the character carries, {"item": REF},
 * as the spell prompts list them, with "usable" true for the spells the
 * character can cast now.  "noun" is what the book's realm calls a spell.
 */
static void net_query_spells(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	struct object *obj = NULL;
	struct net_jw w;

	if (player && player->upkeep && player->class && args) {
		obj = net_resolve_item(net_json_get(args, "item"));
	}
	net_answer_begin(&w, "SPELLS", v);
	if (obj && player_object_to_book(player, obj)) {
		int *spells = NULL;
		int i, n = spell_collect_from_book(player, obj, &spells);

		net_jw_kstr(&w, "noun", net_spell_noun(obj));
		net_jw_key(&w, "spells");
		net_jw_arr(&w);
		for (i = 0; i < n; i++) {
			net_write_spell(&w, spells[i],
				spell_okay_to_cast(player, spells[i]));
		}
		net_jw_arr_end(&w);
		mem_free(spells);
	}
	net_json_end(&w);
}

/**
 * ------------------------------------------------------------------------
 * Stores
 *
 * STORE tells the store screen what the store sells and what it would
 * buy, working each out as the store menu does (store_purchase() and
 * store_sell() in ui-store.c, which this must be kept in step with).
 * ------------------------------------------------------------------------ */

/**
 * The price of each number of an object up to max, so prices[n - 1] is
 * what n cost: bought from the store, or sold to it with store_buying.
 */
static void net_write_prices(struct net_jw *w, struct store *store,
		struct object *obj, bool store_buying, int max)
{
	int n;

	net_jw_key(w, "prices");
	net_jw_arr(w);
	for (n = 1; n <= max; n++) {
		if (store_buying) {
			struct object body = OBJECT_NULL;

			object_copy_amt(&body, obj, n);
			net_jw_int(w, price_item(store, &body, true, n));
			object_wipe(&body);
		} else {
			struct object *dummy = object_new();

			object_copy_amt(dummy, obj, n);
			net_jw_int(w, price_item(store, dummy, false, n));
			object_delete(NULL, NULL, &dummy);
		}
	}
	net_jw_arr_end(w);
}

/**
 * The most of a store's object the character can buy or take now, as
 * store_purchase() works it out, or 0 with why not.  have is how many the
 * pack holds already, when the character may know.
 */
static int net_buy_max(struct store *store, struct object *obj,
		const char **why, int *have)
{
	bool home = store->feat == FEAT_HOME;
	bool aware = object_flavor_is_aware(obj);
	int amt;

	*why = NULL;
	*have = (!aware && !home) ? 0 : find_inven(obj);
	if (home) {
		amt = obj->number;
	} else {
		int price = price_item(store, obj, false, 1);

		if ((uint32_t)player->au < (uint32_t)price) {
			*why = "You do not have enough gold for this item.";
			return 0;
		}
		amt = price ? player->au / price : obj->number;
		if (amt > obj->number) amt = obj->number;
		if (player->au >= price_item(store, obj, false, amt + 1)
				&& amt < obj->number) {
			amt++;
		}
	}
	amt = MIN(amt, inven_carry_num(player, obj));
	if (amt <= 0 || (!aware && !home && pack_is_full())) {
		*why = "You cannot carry that many items.";
		return 0;
	}
	return amt;
}

/**
 * The most of a carried object the store would buy, or the home take, as
 * store_sell() works it out, or 0 with why not.
 */
static int net_sell_max(struct store *store, struct object *obj,
		const char **why)
{
	bool home = store->feat == FEAT_HOME;
	struct object body = OBJECT_NULL;
	bool room;

	*why = NULL;
	if (!home && !store_will_buy_tester(obj)) {
		*why = "I do not wish to purchase this item.";
		return 0;
	}
	if (object_is_equipped(player->body, obj) && !obj_can_takeoff(obj)) {
		*why = "Hmmm, it seems to be stuck.";
		return 0;
	}
	object_copy_amt(&body, obj, 1);
	room = store_check_num(store, &body);
	object_wipe(&body);
	if (!room) {
		*why = home ? "Your home is full." :
			"I have not the room in my store to keep it.";
		return 0;
	}
	return obj->number;
}

static void net_write_sellable(struct net_jw *w, struct store *store,
		struct object *obj, const char *where, int index)
{
	const char *why;
	int max = net_sell_max(store, obj, &why);

	net_jw_obj(w);
	net_object_fields(w, obj, where, index);
	net_jw_kint(w, "max", max);
	if (why) net_jw_kstr(w, "why", why);
	if (max > 0 && store->feat != FEAT_HOME) {
		net_write_prices(w, store, obj, true, max);
	}
	net_jw_obj_end(w);
}

/**
 * STORE describes the store the character is in, as "store": its name, its owner and
 * the most the owner pays for one object ("purse"), whether it is the
 * home, and the character's gold.  "stock" lists what it sells, each with
 * the most the character can buy now as "max", or "why" not, how many the
 * pack already holds as "have", and the price of each number up to max
 * as "prices".  "gear" lists the pack, the quiver, and the equipment, each
 * with the most the store would buy, or why not, and what it would pay.
 * The home has no prices.  "no_selling" says the store pays nothing, as
 * the birth option of that name has it.
 */
static void net_query_store(const struct net_json *v)
{
	struct store *store = NULL;
	struct net_jw w;

	if (player && player->upkeep && character_dungeon && cave) {
		store = store_at(cave, player->grid);
	}
	net_answer_begin(&w, "STORE", v);
	if (store) {
		bool home = store->feat == FEAT_HOME;
		struct object **list = mem_zalloc(z_info->store_inven_max
			* sizeof(*list));
		int i;

		net_jw_key(&w, "store");
		net_jw_obj(&w);
		net_jw_kstr(&w, "name", f_info[store->feat].name);
		if (!home) {
			net_jw_kstr(&w, "owner", store->owner->name);
			net_jw_kint(&w, "purse", store->owner->max_cost);
		}
		net_jw_kbool(&w, "home", home);
		net_jw_kint(&w, "gold", player->au);
		net_jw_kbool(&w, "no_selling", OPT(player, birth_no_selling));

		store_stock_list(store, list, z_info->store_inven_max);
		net_jw_key(&w, "stock");
		net_jw_arr(&w);
		for (i = 0; i < z_info->store_inven_max && list[i]; i++) {
			const char *why;
			int have, max = net_buy_max(store, list[i], &why, &have);

			net_jw_obj(&w);
			net_object_fields_as(&w, list[i], "store", i, ODESC_PREFIX
				| ODESC_FULL | (home ? 0 : ODESC_STORE));
			net_jw_kint(&w, "max", max);
			if (why) net_jw_kstr(&w, "why", why);
			if (have) net_jw_kint(&w, "have", have);
			if (!home) {
				net_jw_kint(&w, "price",
					price_item(store, list[i], false, 1));
				if (max > 0) {
					net_write_prices(&w, store, list[i], false, max);
				}
			}
			net_jw_obj_end(&w);
		}
		net_jw_arr_end(&w);
		mem_free(list);

		net_jw_key(&w, "gear");
		net_jw_arr(&w);
		for (i = 0; i < z_info->pack_size; i++) {
			struct object *obj = player->upkeep->inven[i];

			if (obj) net_write_sellable(&w, store, obj, "inven", i);
		}
		for (i = 0; i < z_info->quiver_size; i++) {
			struct object *obj = player->upkeep->quiver[i];

			if (obj) net_write_sellable(&w, store, obj, "quiver", i);
		}
		for (i = 0; i < player->body.count; i++) {
			struct object *obj = slot_object(player, i);

			if (obj) net_write_sellable(&w, store, obj, "equip", i);
		}
		net_jw_arr_end(&w);
		net_jw_obj_end(&w);
	}
	net_json_end(&w);
}

/**
 * ------------------------------------------------------------------------
 * Character creation
 * ------------------------------------------------------------------------ */

/**
 * The skills the birth menus show for a race, or a race and class
 * together, as skill_help() in ui-birth.c does.
 */
static void net_skill_text(textblock *tb, const int r_skills[],
		const int c_skills[], int mhp, int exp, int infra)
{
	int skills[SKILL_MAX];
	int i;

	for (i = 0; i < SKILL_MAX; i++) {
		skills[i] = (r_skills ? r_skills[i] : 0)
			+ (c_skills ? c_skills[i] : 0);
	}
	textblock_append(tb, "Hit/Shoot/Throw: %+d/%+d/%+d\n",
		skills[SKILL_TO_HIT_MELEE], skills[SKILL_TO_HIT_BOW],
		skills[SKILL_TO_HIT_THROW]);
	textblock_append(tb, "Hit die: %2d   XP mod: %d%%\n", mhp, exp);
	textblock_append(tb, "Disarm: %+3d/%+3d   Devices: %+3d\n",
		skills[SKILL_DISARM_PHYS], skills[SKILL_DISARM_MAGIC],
		skills[SKILL_DEVICE]);
	textblock_append(tb, "Save:   %+3d   Stealth: %+3d\n", skills[SKILL_SAVE],
		skills[SKILL_STEALTH]);
	if (infra >= 0) {
		textblock_append(tb, "Infravision:  %d ft\n", infra * 10);
	}
	textblock_append(tb, "Digging:      %+d\n", skills[SKILL_DIGGING]);
	textblock_append(tb, "Search:       %+d\n", skills[SKILL_SEARCH]);
}

/**
 * A race's or class's stat changes, as the birth menus show them: two to
 * a line.
 */
static void net_stat_adj_text(textblock *tb, const int adj[STAT_MAX])
{
	int len = (STAT_MAX + 1) / 2;
	int i;

	for (i = 0; i < len; i++) {
		textblock_append(tb, "%s%+3d", stat_names_reduced[i], adj[i]);
		if (i + len < STAT_MAX) {
			textblock_append(tb, "  %s%+3d", stat_names_reduced[i + len],
				adj[i + len]);
		}
		textblock_append(tb, "\n");
	}
}

/**
 * What a race is good at, as race_help() in ui-birth.c shows it, which
 * this must be kept in step with.  It lists every ability, where the menu
 * has room for three.
 */
static void net_race_text(textblock *tb, const struct player_race *r)
{
	struct player_ability *a;

	net_stat_adj_text(tb, r->r_adj);
	textblock_append(tb, "\n");
	net_skill_text(tb, r->r_skills, NULL, r->r_mhp, r->r_exp, r->infra);
	for (a = player_abilities; a; a = a->next) {
		if (streq(a->type, "object") && !of_has(r->flags, a->index)) continue;
		if (streq(a->type, "player") && !pf_has(r->pflags, a->index)) continue;
		if (streq(a->type, "element")
				&& r->el_info[a->index].res_level != a->value) {
			continue;
		}
		textblock_append_c(tb, COLOUR_L_GREEN, "\n%s", a->name);
	}
}

/**
 * What a class is good at with race r, as class_help() in ui-birth.c
 * shows it, which this must be kept in step with.
 */
static void net_class_text(textblock *tb, const struct player_class *c,
		const struct player_race *r)
{
	struct player_ability *a;
	int adj[STAT_MAX];
	int i;

	for (i = 0; i < STAT_MAX; i++) adj[i] = c->c_adj[i] + r->r_adj[i];
	net_stat_adj_text(tb, adj);
	textblock_append(tb, "\n");
	net_skill_text(tb, r->r_skills, c->c_skills, r->r_mhp + c->c_mhp,
		r->r_exp + c->c_exp, -1);
	if (c->magic.total_spells) {
		int count;
		struct magic_realm *realm = class_magic_realms(c, &count);
		char buf[120] = "";

		while (realm) {
			struct magic_realm *next = realm->next;

			if (buf[0]) my_strcat(buf, next ? ", " : " and ", sizeof(buf));
			my_strcat(buf, realm->name, sizeof(buf));
			mem_free(realm);
			realm = next;
		}
		textblock_append_c(tb, COLOUR_L_BLUE, "\nLearns %s magic", buf);
	}
	for (a = player_abilities; a; a = a->next) {
		if (streq(a->type, "object") && !of_has(c->flags, a->index)) continue;
		if (streq(a->type, "player") && !pf_has(c->pflags, a->index)) continue;
		if (streq(a->type, "element")) continue;
		textblock_append_c(tb, COLOUR_L_GREEN, "\n%s", a->name);
	}
}

static void net_jw_text(struct net_jw *w, const char *key, textblock *tb)
{
	net_jw_key(w, key);
	net_jw_textblock(w, tb);
	textblock_free(tb);
}

/**
 * BIRTH tells the birth screen what the character being made looks like,
 * as "birth": the races and classes, each with what it is good at (a class with the
 * chosen race), the chosen ones, the stats, the points left to buy them
 * with or whether the roller made them, hit points, gold, age, height,
 * weight, name, and history.  "quickstart" says the previous character
 * can be made again as it was, and "force_name" that the name cannot
 * change.  With {"name": NAME} it also says whether a savefile with that
 * name exists, as "name_used".
 */
static void net_query_birth(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "name") : NULL;
	struct net_jw w;
	int i;

	net_answer_begin(&w, "BIRTH", v);
	if (net_ctx == CTX_BIRTH && player && player->race && player->class) {
		/* Have the game report the points again, as the birth menus do
		 * when they show them; it changes nothing */
		do_cmd_refresh_stats(NULL);

		net_jw_key(&w, "birth");
		net_jw_obj(&w);
		net_jw_kbool(&w, "quickstart", birth_quickstart);
		net_jw_kbool(&w, "force_name", arg_force_name);
		net_jw_key(&w, "races");
		net_jw_arr(&w);
		for (i = 0; player_id2race(i); i++) {
			textblock *tb = textblock_new();

			net_race_text(tb, player_id2race(i));
			net_jw_obj(&w);
			net_jw_kstr(&w, "name", player_id2race(i)->name);
			net_jw_text(&w, "help", tb);
			net_jw_obj_end(&w);
		}
		net_jw_arr_end(&w);
		net_jw_key(&w, "classes");
		net_jw_arr(&w);
		for (i = 0; player_id2class(i); i++) {
			textblock *tb = textblock_new();

			net_class_text(tb, player_id2class(i), player->race);
			net_jw_obj(&w);
			net_jw_kstr(&w, "name", player_id2class(i)->name);
			net_jw_text(&w, "help", tb);
			net_jw_obj_end(&w);
		}
		net_jw_arr_end(&w);
		net_jw_kint(&w, "race", player->race->ridx);
		net_jw_kint(&w, "class", player->class->cidx);

		net_jw_key(&w, "stats");
		net_jw_arr(&w);
		for (i = 0; i < STAT_MAX; i++) {
			int self = player->stat_max[i];

			net_jw_obj(&w);
			/* "Str: " without its colon */
			net_jw_key(&w, "name");
			net_jw_strn(&w, stat_names_reduced[i],
				strcspn(stat_names_reduced[i], ":"));
			net_jw_kint(&w, "self", self);
			net_jw_kint(&w, "race", player->race->r_adj[i]);
			net_jw_kint(&w, "class", player->class->c_adj[i]);
			net_jw_kint(&w, "best", player->state.stat_top[i]);
			if (!birth_rolled) {
				net_jw_kint(&w, "spent", birth_spent[i]);
				/* The cost table stops at 18 */
				if (self < 18) net_jw_kint(&w, "cost", birth_cost[i]);
			}
			net_jw_obj_end(&w);
		}
		net_jw_arr_end(&w);
		net_jw_kbool(&w, "rolled", birth_rolled);
		if (!birth_rolled) net_jw_kint(&w, "points", birth_left);

		net_jw_kint(&w, "hp", player->mhp);
		net_jw_kint(&w, "gold", player->au);
		net_jw_kint(&w, "age", player->age);
		net_jw_kint(&w, "height", player->ht);
		net_jw_kint(&w, "weight", player->wt);
		net_jw_kstr(&w, "name", player->full_name);
		net_jw_kstr(&w, "history", player->history ? player->history : "");
		if (name) {
			net_jw_kbool(&w, "name_used", !savefile[0]
				&& savefile_name_already_used(name, true, true));
		}
		net_jw_obj_end(&w);
	}
	net_json_end(&w);
}

/**
 * ------------------------------------------------------------------------
 * Knowledge, the character sheet, messages, and help
 * ------------------------------------------------------------------------ */

/* The knowledge menu's lists, as KNOWLEDGE and RECALL name them */
static const char *know_names[KNOW_SECTIONS] = {
	"objects", "runes", "artifacts", "egos", "monsters", "features",
	"traps", "shapes"
};

static bool know_section(const char *name, enum knowledge_section *out)
{
	int i;

	for (i = 0; name && i < KNOW_SECTIONS; i++) {
		if (streq(name, know_names[i])) {
			*out = (enum knowledge_section)i;
			return true;
		}
	}
	return false;
}

/* A store's number from "store:N", or -1 */
static int know_store(const char *name)
{
	int n;

	if (!name || !prefix(name, "store:")) return -1;
	n = atoi(name + strlen("store:"));
	return (n >= 0 && n < z_info->store_max) ? n : -1;
}

/*
 * How the knowledge menu shows an entry, as fields of the object being
 * written: "char" and "color", and with tiles its "tile".  As elsewhere,
 * with tiles and in headless mode the letter is the entry's own.
 */
static void net_write_entry_glyph(struct net_jw *w,
		const struct knowledge_entry *e)
{
	bool tile = !net_headless && use_graphics != GRAPHICS_NONE
		&& (e->attr & 0x80);
	bool own = tile || net_headless;
	wchar_t c = own ? e->d_char : e->ch;
	char ch[5];

	if (!e->glyph || !c) return;
	net_utf8(c, ch);
	net_jw_kstr(w, "char", ch);
	net_jw_kint(w, "color", own ? e->d_attr : e->attr);
	if (tile) {
		net_jw_key(w, "tile");
		net_jw_obj(w);
		net_jw_kint(w, "row", e->attr & 0x7F);
		net_jw_kint(w, "col", e->ch & 0x7F);
		net_jw_obj_end(w);
	}
}

/*
 * The character as the map shows it (grid_data_as_text() in ui-map.c), as
 * fields of the object being written.
 */
static void net_write_player_glyph(struct net_jw *w)
{
	struct knowledge_entry e;

	memset(&e, 0, sizeof(e));
	e.glyph = true;
	e.attr = monster_x_attr[0];
	e.ch = monster_x_char[0];
	e.d_attr = r_info[0].d_attr;
	e.d_char = r_info[0].d_char;
	net_write_entry_glyph(w, &e);
}

/*
 * The entry the Tile Editor changes for a knowledge entry, by its kind and
 * id in VISUALS, as "visual"; nothing for an entry with no tile.
 */
static void net_write_entry_visual(struct net_jw *w,
		enum knowledge_section section, const struct knowledge_entry *e)
{
	const char *kind = NULL;
	int id = e->id;

	switch (section) {
		case KNOW_MONSTERS: kind = "monster"; break;
		case KNOW_FEATURES: kind = "terrain"; break;
		case KNOW_TRAPS: kind = "trap"; break;
		case KNOW_OBJECTS: {
			const struct object_kind *k = &k_info[id];

			/* The slot the map's tile comes from (object_kind_attr()) */
			if (k->flavor && !(k->tval == TV_SCROLL && k->aware)) {
				kind = "flavor";
				id = k->flavor->fidx;
			} else {
				kind = "object";
			}
			break;
		}
		case KNOW_ARTIFACTS: {
			const struct object_kind *k =
				lookup_kind(a_info[id].tval, a_info[id].sval);

			if (k) {
				kind = "object";
				id = k->kidx;
			}
			break;
		}
		default:
			break;
	}
	if (!kind) return;
	net_jw_key(w, "visual");
	net_jw_obj(w);
	net_jw_kstr(w, "kind", kind);
	net_jw_kint(w, "id", id);
	net_jw_obj_end(w);
}

struct know_writer {
	struct net_jw *w;
	enum knowledge_section section;
};

static void net_write_entry(const struct knowledge_entry *e, void *data)
{
	struct know_writer *kw = data;
	struct net_jw *w = kw->w;

	net_jw_obj(w);
	net_jw_kstr(w, "group", e->group ? e->group : "");
	net_jw_kint(w, "id", e->id);
	net_jw_kstr(w, "name", e->name);
	if (e->note[0]) net_jw_kstr(w, "note", e->note);
	if (e->dim) net_jw_kbool(w, "dim", true);
	net_write_entry_glyph(w, e);
	net_write_entry_visual(w, kw->section, e);
	net_jw_obj_end(w);
}

/* A store's stock, as the knowledge menu lists it (textui_store_knowledge()) */
static void net_write_store_knowledge(struct net_jw *w, int n)
{
	struct store *store = &stores[n];
	struct object **list = mem_zalloc(z_info->store_inven_max * sizeof(*list));
	int count = MIN(store->stock_num, z_info->store_inven_max), i;

	store_stock_list(store, list, z_info->store_inven_max);
	for (i = 0; i < count && list[i]; i++) {
		char name[120];

		object_desc(name, sizeof(name), list[i], ODESC_PREFIX | ODESC_FULL,
			player);
		net_jw_obj(w);
		net_jw_kstr(w, "group", "");
		net_jw_kint(w, "id", i);
		net_jw_kstr(w, "name", name);
		if (store->feat != FEAT_HOME) {
			net_jw_kstr(w, "note", format("%d gold",
				price_item(store, list[i], false, 1)));
		}
		net_write_glyph(w, list[i]);
		net_jw_obj_end(w);
	}
	mem_free(list);
}

/* The character's history, as history_display() shows it */
static void net_write_history(struct net_jw *w)
{
	struct history_info *list = NULL;
	size_t n = history_get_list(player, &list), i;

	for (i = 0; i < n; i++) {
		char event[100];

		my_strcpy(event, list[i].event, sizeof(event));
		if (hist_has(list[i].type, HIST_ARTIFACT_LOST)) {
			my_strcat(event, " (LOST)", sizeof(event));
		}
		net_jw_obj(w);
		net_jw_kstr(w, "group", "");
		net_jw_kint(w, "id", (int)i);
		net_jw_kstr(w, "name", event);
		net_jw_kint(w, "turn", list[i].turn);
		net_jw_kint(w, "depth", list[i].dlev * 50);
		net_jw_kint(w, "level", list[i].clev);
		net_jw_obj_end(w);
	}
}

/*
 * The hall of fame, as predict_score() and display_score_page() show it:
 * every score, with where the character would place if it died now
 * marked "current".  Each has its first line as "name", its score as
 * "note", and its other two lines as "lines".
 */
static void net_write_scores(struct net_jw *w)
{
	struct high_score scores[MAX_HISCORES], the_score;
	size_t n, j, i;

	n = highscore_read(scores, N_ELEMENTS(scores));
	build_score(&the_score, player, "nobody (yet!)", NULL);
	if (player->is_dead) {
		j = highscore_where(&the_score, scores, N_ELEMENTS(scores));
	} else {
		j = highscore_add(&the_score, scores, N_ELEMENTS(scores));
		if (n < N_ELEMENTS(scores)) n++;
	}
	for (i = 0; i < n && scores[i].what[0]; i++) {
		const struct high_score *s = &scores[i];
		struct player_class *c = player_id2class(atoi(s->p_c));
		struct player_race *r = player_id2race(atoi(s->p_r));
		int clev = atoi(s->cur_lev), mlev = atoi(s->max_lev);
		int cdun = atoi(s->cur_dun), mdun = atoi(s->max_dun);
		const char *user = s->uid, *when = s->day, *gold = s->gold;
		const char *aged = s->turns, *pts = s->pts;
		char buf[160], date[16];

		while (isspace((unsigned char)*user)) user++;
		while (isspace((unsigned char)*when)) when++;
		while (isspace((unsigned char)*gold)) gold++;
		while (isspace((unsigned char)*aged)) aged++;
		while (isspace((unsigned char)*pts)) pts++;

		net_jw_obj(w);
		net_jw_kstr(w, "group", "");
		net_jw_kint(w, "id", (int)i);
		strnfmt(buf, sizeof(buf), "%s the %s %s, level %d", s->who,
			r ? r->name : "<none>", c ? c->name : "<none>", clev);
		if (mlev > clev) my_strcat(buf, format(" (Max %d)", mlev), sizeof(buf));
		net_jw_kstr(w, "name", buf);
		net_jw_kstr(w, "note", pts);
		if (i == j) net_jw_kbool(w, "current", true);
		net_jw_key(w, "lines");
		net_jw_arr(w);
		if (!cdun) {
			strnfmt(buf, sizeof(buf), "Killed by %s in the town", s->how);
		} else {
			strnfmt(buf, sizeof(buf), "Killed by %s on dungeon level %d",
				s->how, cdun);
		}
		if (mdun > cdun) my_strcat(buf, format(" (Max %d)", mdun), sizeof(buf));
		net_jw_str(w, buf);
		if (*when == '@' && strlen(when) == 9) {
			strnfmt(date, sizeof(date), "%.4s-%.2s-%.2s", when + 1,
				when + 5, when + 7);
			when = date;
		}
		strnfmt(buf, sizeof(buf), "(User %s, Date %s, Gold %s, Turn %s).",
			user, when, gold, aged);
		net_jw_str(w, buf);
		net_jw_arr_end(w);
		net_jw_obj_end(w);
	}
}

/**
 * KNOWLEDGE lists the sections of the knowledge menu (ui-knowledge.c) as
 * "sections", each with "name", "title", and whether it has anything in
 * it yet, as "known".  With {"section": NAME} it gives that section's
 * entries as "list" instead, in the menu's order: each has "group", "id" (what
 * RECALL takes with the section's name), "name", and maybe "note" (kills,
 * an inscription, a price), "dim" (not fully known), how it looks, and
 * the "visual" the Tile Editor changes.  The sections are "objects",
 * "runes", "artifacts", "egos", "monsters", "features", "traps",
 * "shapes", "store:N" for each store's stock, "history", and "scores".
 * History entries also have "turn", "depth" in feet, and "level"; scores
 * have "lines" and mark the place the character would take now as
 * "current".  "equipment", the equipment comparison, has no entries,
 * since only the classic screen shows it: the knowledge screen's REPLY
 * {"classic": "equipment"} opens it, and the screen then opens again with
 * "view" "equipment".
 */
static void net_query_knowledge(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "section") : NULL;
	enum knowledge_section section;
	struct net_jw w;
	int i;

	net_answer_begin(&w, "KNOWLEDGE", v);
	if (!player || !player->upkeep || !character_generated) {
		net_json_end(&w);
		return;
	}
	if (!name) {
		net_jw_key(&w, "sections");
		net_jw_arr(&w);
		for (i = 0; i < KNOW_SECTIONS; i++) {
			net_jw_obj(&w);
			net_jw_kstr(&w, "name", know_names[i]);
			net_jw_kstr(&w, "title", knowledge_section_title(i));
			net_jw_kbool(&w, "known", knowledge_section_known(i));
			net_jw_obj_end(&w);
		}
		for (i = 0; i < z_info->store_max; i++) {
			const char *fname = f_info[stores[i].feat].name;

			net_jw_obj(&w);
			net_jw_kstr(&w, "name", format("store:%d", i));
			net_jw_kstr(&w, "title", fname ? fname : format("Store %d", i + 1));
			net_jw_kbool(&w, "known", true);
			net_jw_obj_end(&w);
		}
		net_jw_obj(&w);
		net_jw_kstr(&w, "name", "scores");
		net_jw_kstr(&w, "title", "Hall of fame");
		net_jw_kbool(&w, "known", true);
		net_jw_obj_end(&w);
		net_jw_obj(&w);
		net_jw_kstr(&w, "name", "history");
		net_jw_kstr(&w, "title", "History");
		net_jw_kbool(&w, "known", true);
		net_jw_obj_end(&w);
		net_jw_obj(&w);
		net_jw_kstr(&w, "name", "equipment");
		net_jw_kstr(&w, "title", "Equipment comparison");
		net_jw_kbool(&w, "known", true);
		net_jw_obj_end(&w);
		net_jw_arr_end(&w);
		net_json_end(&w);
		return;
	}

	net_jw_kstr(&w, "section", name);
	net_jw_key(&w, "list");
	net_jw_arr(&w);
	if (know_section(name, &section)) {
		struct know_writer kw = { &w, section };

		knowledge_list(section, net_write_entry, &kw);
	} else if (know_store(name) >= 0) {
		net_write_store_knowledge(&w, know_store(name));
	} else if (streq(name, "history")) {
		net_write_history(&w);
	} else if (streq(name, "scores")) {
		net_write_scores(&w);
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
}

/*
 * Draw something the text UI draws onto a term of the main term's size
 * that nobody sees, and return what it drew as styled text, without its
 * blank lines at the top and bottom.  Tiles come out as spaces.
 */
/*
 * Draw where nobody sees: on a term of the main term's width, and of its
 * height or hgt rows when hgt is more, until net_offscreen_end().
 */
static void net_offscreen_begin(term *t, term **old, int hgt)
{
	int wid = 80;

	hgt = MAX(hgt, 24);
	if (term_screen) {
		wid = MAX(term_screen->wid, 80);
		hgt = MAX(term_screen->hgt, hgt);
	}
	*old = Term;
	term_init(t, wid, hgt, 1);
	Term_activate(t);
}

static void net_offscreen_end(term *t, term *old)
{
	Term_activate(old);
	term_nuke(t);
}

/*
 * Rows y0 to y1 - 1 of a term as styled text, without blank rows at the
 * end.  Tiles come out as spaces.
 */
static textblock *net_term_text(term *t, int y0, int y1)
{
	textblock *tb = textblock_new();
	struct net_buf run;
	int x, y, last = y0 - 1;

	y1 = MIN(y1, t->hgt);
	for (y = y0; y < y1; y++) {
		for (x = 0; x < t->wid; x++) {
			if (t->scr->c[y][x] && t->scr->c[y][x] != L' '
					&& !(t->scr->a[y][x] & 0x80)) {
				last = y;
				break;
			}
		}
	}
	net_buf_init(&run);
	for (y = y0; y <= last; y++) {
		int end = t->wid;

		while (end > 0 && (!t->scr->c[y][end - 1]
				|| t->scr->c[y][end - 1] == L' '
				|| (t->scr->a[y][end - 1] & 0x80))) {
			end--;
		}
		for (x = 0; x < end; ) {
			int a = t->scr->a[y][x] & 0x7F;

			run.len = 0;
			while (x < end && (t->scr->a[y][x] & 0x7F) == a) {
				wchar_t c = (t->scr->a[y][x] & 0x80) ? L' ' : t->scr->c[y][x];

				net_put_utf8(&run, (uint32_t)(c ? c : L' '));
				x++;
			}
			net_buf_append(&run, "", 1);
			textblock_append_c(tb, (uint8_t)a, "%s", (const char *)run.data);
		}
		if (y < last) textblock_append(tb, "\n");
	}
	net_buf_free(&run);
	return tb;
}

struct panel_writer {
	struct net_jw *w;
	/* The panel whose list is open, or -1 */
	int open;
};

static void net_player_panel_line(int panel, uint8_t attr, const char *label,
		const char *value, void *data)
{
	struct panel_writer *pw = data;
	struct net_jw *w = pw->w;

	if (panel != pw->open) {
		if (pw->open >= 0) net_jw_arr_end(w);
		net_jw_arr(w);
		pw->open = panel;
	}
	net_jw_obj(w);
	if (label) {
		net_jw_kstr(w, "label", label);
		net_jw_kstr(w, "value", value);
		net_jw_kint(w, "color", attr);
	}
	net_jw_obj_end(w);
}

/**
 * CHARACTER gives the character sheet (ui-player.c) as "character": the
 * sheet's "panels", each a list of lines with "label", "value", and
 * "color" ({} for a blank line), in the order player_sheet_panels() gives
 * them; the "stats" table, as display_player_stat_info() shows it; the
 * "history"; the table of resistances and abilities by equipment slot,
 * as display_player_ability_table() draws it, as styled text: its two
 * heading rows, the equipment and the slots' letters, as
 * "abilities_head", the rest as "abilities", and the column its slots
 * start at as "slot_column"; "equippy", the equipment slot by slot; and "player", the character's
 * letter and tile, as the map shows them.  "force_name" says the name
 * cannot change.
 */
static void net_query_character(const struct net_json *v)
{
	struct net_jw w;
	struct panel_writer pw;
	int i;

	net_answer_begin(&w, "CHARACTER", v);
	if (!player || !player->upkeep || !character_generated || !player->race) {
		net_jw_key(&w, "character");
		net_jw_null(&w);
		net_json_end(&w);
		return;
	}
	net_jw_key(&w, "character");
	net_jw_obj(&w);
	net_jw_kstr(&w, "name", player->full_name);
	net_jw_kbool(&w, "force_name", arg_force_name);

	net_jw_key(&w, "panels");
	net_jw_arr(&w);
	pw.w = &w;
	pw.open = -1;
	player_sheet_panels(net_player_panel_line, &pw);
	if (pw.open >= 0) net_jw_arr_end(&w);
	net_jw_arr_end(&w);

	net_jw_key(&w, "stats");
	net_jw_arr(&w);
	for (i = 0; i < STAT_MAX; i++) {
		char buf[32];
		bool drained = player->stat_cur[i] < player->stat_max[i];

		net_jw_obj(&w);
		net_jw_kstr(&w, "name", drained ? stat_names_reduced[i] : stat_names[i]);
		if (drained) net_jw_kbool(&w, "drained", true);
		if (player->stat_max[i] == 18 + 100) net_jw_kbool(&w, "maxed", true);
		cnv_stat(player->stat_max[i], buf, sizeof(buf));
		net_jw_kstr(&w, "self", buf);
		net_jw_kint(&w, "race", player->race->r_adj[i]);
		net_jw_kint(&w, "class", player->class->c_adj[i]);
		net_jw_kint(&w, "equip", player->state.stat_add[i]);
		cnv_stat(player->state.stat_top[i], buf, sizeof(buf));
		net_jw_kstr(&w, "best", buf);
		if (drained) {
			cnv_stat(player->state.stat_use[i], buf, sizeof(buf));
			net_jw_kstr(&w, "current", buf);
		}
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);

	net_jw_kstr(&w, "history", player->history ? player->history : "");
	/* The table of resistances and abilities, as the game draws it */
	{
		term *old, t;
		int rows;

		net_offscreen_begin(&t, &old, 200);
		rows = display_player_ability_table();
		Term_activate(old);
		net_jw_text(&w, "abilities_head", net_term_text(&t, 0, 2));
		net_jw_text(&w, "abilities", net_term_text(&t, 2, rows));
		net_jw_kint(&w, "slot_column", PLAYER_TABLE_LABEL);
		term_nuke(&t);
	}
	net_write_equippy(&w);

	if (r_info && r_info[0].name) {
		net_jw_key(&w, "player");
		net_jw_obj(&w);
		net_write_player_glyph(&w);
		net_jw_obj_end(&w);
	}
	net_jw_obj_end(&w);
	net_json_end(&w);
}

/* Message type names, as list-message.h gives them */
static const char *net_message_names[] = {
	#define MSG(x, s) #x,
	#include "list-message.h"
	#undef MSG
};

/**
 * MESSAGES gives the message history, oldest first, as "messages": each
 * with "text", how many times in a row it came as "count" when more than
 * once, its "type" by number and "kind" by name (from list-message.h),
 * and the "color" the message log shows it in.
 */
static void net_query_messages(const struct net_json *v)
{
	struct net_jw w;
	int n = messages_num(), i;

	net_answer_begin(&w, "MESSAGES", v);
	net_jw_key(&w, "messages");
	net_jw_arr(&w);
	for (i = n - 1; i >= 0; i--) {
		uint16_t type = message_type(i);

		net_jw_obj(&w);
		net_jw_kstr(&w, "text", message_str(i));
		if (message_count(i) > 1) net_jw_kint(&w, "count", message_count(i));
		net_jw_kint(&w, "type", type);
		if (type < N_ELEMENTS(net_message_names)) {
			net_jw_kstr(&w, "kind", net_message_names[type]);
		}
		net_jw_kint(&w, "color", message_color(i));
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
}

static bool net_blank(const char *s)
{
	for (; *s; s++) {
		if (!isspace((unsigned char)*s)) return false;
	}
	return true;
}

/* Whether a line underlines the one above it, as "====" does */
static bool net_underline(const char *s)
{
	size_t n = strlen(s), i;

	if (n < 3 || (s[0] != '=' && s[0] != '-')) return false;
	for (i = 1; i < n; i++) {
		if (s[i] != s[0] && !isspace((unsigned char)s[i])) return false;
	}
	return true;
}

/**
 * HELP gives a help file from the help folder as show_file() in
 * ui-help.c reads it, which this must be kept in step with: {"file":
 * NAME}, or with no file the index the game's own help opens for the
 * keyset in use.  The answer has the "file", its "page" as styled text,
 * with directives left out and headings (a line underlined with "=" or
 * "-") in color in place of their underlines; its "links", each a "key"
 * and the "file" that key opens; and its "tags", each a "name" and the
 * line of "page" it marks.  "page" is null when there is no such file.
 */
static void net_query_help(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "file") : NULL;
	char path[1024], buf[1024], next[1024];
	struct net_jw w;
	ang_file *f = NULL;
	textblock *tb;
	bool skip = false, have_next;
	int line = 0, i;
	struct { char key; char file[32]; } links[26];
	struct { char name[64]; int line; } tags[64];
	int n_links = 0, n_tags = 0;

	if (!name) {
		name = (player && OPT(player, rogue_like_commands)) ?
			"r_index.txt" : "index.txt";
	}
	net_answer_begin(&w, "HELP", v);
	net_jw_kstr(&w, "file", name);

	/* Only the help folder's own files */
	if (!strchr(name, '/') && !strchr(name, '\\') && !strstr(name, "..")) {
		path_build(path, sizeof(path), ANGBAND_DIR_HELP, name);
		f = file_open(path, MODE_READ, FTYPE_TEXT);
	}
	if (!f) {
		net_jw_key(&w, "page");
		net_jw_null(&w);
		net_json_end(&w);
		return;
	}

	tb = textblock_new();
	have_next = file_getl(f, next, sizeof(next));
	while (have_next) {
		my_strcpy(buf, next, sizeof(buf));
		have_next = file_getl(f, next, sizeof(next));

		/* A directive, and the lines after it up to a blank one */
		if (skip) {
			if (net_blank(buf)) skip = false;
			continue;
		}
		if (prefix(buf, ".. ")) {
			if (prefix(buf + 3, "menu:: [") && buf[strlen(".. menu:: [x")] == ']'
					&& n_links < 26) {
				links[n_links].key = buf[strlen(".. menu:: [")];
				my_strcpy(links[n_links].file, buf + strlen(".. menu:: [x] "),
					sizeof(links[0].file));
				n_links++;
			} else if (buf[3] == '_' && n_tags < 64) {
				size_t len = strlen(buf);

				/* ".. _tag:" */
				if (len > 5 && buf[len - 1] == ':') buf[len - 1] = '\0';
				my_strcpy(tags[n_tags].name, buf + 4, sizeof(tags[0].name));
				tags[n_tags].line = line;
				n_tags++;
			}
			skip = true;
			continue;
		}

		if (line > 0) textblock_append(tb, "\n");
		if (have_next && !net_blank(buf) && net_underline(next)) {
			textblock_append_c(tb, COLOUR_YELLOW, "%s", buf);
			have_next = file_getl(f, next, sizeof(next));
		} else {
			textblock_append(tb, "%s", buf);
		}
		line++;
	}
	file_close(f);

	net_jw_key(&w, "page");
	net_jw_textblock(&w, tb);
	textblock_free(tb);
	net_jw_key(&w, "links");
	net_jw_arr(&w);
	for (i = 0; i < n_links; i++) {
		char key[2] = { links[i].key, 0 };

		net_jw_obj(&w);
		net_jw_kstr(&w, "key", key);
		net_jw_kstr(&w, "file", links[i].file);
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	net_jw_key(&w, "tags");
	net_jw_arr(&w);
	for (i = 0; i < n_tags; i++) {
		net_jw_obj(&w);
		net_jw_kstr(&w, "name", tags[i].name);
		net_jw_kint(&w, "line", tags[i].line);
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
}

/**
 * TOMB gives what the game's tombstone shows when a character dies, as
 * display_exit_screen() in ui-death.c draws it, which this must be kept
 * in step with: "name"; "title", the class's title for the level, or
 * "Magnificent" for a winner; "class"; "level"; "exp"; "gold"; "depth",
 * the dungeon level; "died_from", what killed the character; "retired"
 * and "winner"; and "date", the time now as the stone puts it.  "tomb"
 * is null before a character exists.
 */
static void net_query_tomb(const struct net_json *v)
{
	struct net_jw w;
	time_t now = time(NULL);
	char date[32];
	bool retired;

	net_answer_begin(&w, "TOMB", v);
	net_jw_key(&w, "tomb");
	if (!player || !player->race || !player->class || !character_generated) {
		net_jw_null(&w);
		net_json_end(&w);
		return;
	}
	retired = player->died_from[0] && streq(player->died_from, "Retiring");
	my_strcpy(date, ctime(&now), 25);
	net_jw_obj(&w);
	net_jw_kstr(&w, "name", player->full_name);
	net_jw_kstr(&w, "title", player->total_winner ? "Magnificent" :
		player->class->title[(player->lev - 1) / 5]);
	net_jw_kstr(&w, "class", player->class->name);
	net_jw_kint(&w, "level", player->lev);
	net_jw_kint(&w, "exp", player->exp);
	net_jw_kint(&w, "gold", player->au);
	net_jw_kint(&w, "depth", player->depth);
	net_jw_kstr(&w, "died_from", player->died_from);
	net_jw_kbool(&w, "dead", player->is_dead);
	net_jw_kbool(&w, "retired", retired);
	net_jw_kbool(&w, "winner", player->total_winner);
	net_jw_kstr(&w, "date", date);
	net_jw_obj_end(&w);
	net_json_end(&w);
}

/**
 * COMMANDS gives the game's command menu, the one Enter opens, as
 * ui-context.c draws it from cmds_all in ui-game.c: "keyset", "original"
 * or "roguelike"; and "lists", each command list in cmds_all's order,
 * with its "name", its "level" (0 for the groups the menu opens on, more
 * for the submenus), and its "commands", each with its "name", its "key"
 * as the menu shows it in the keyset in use, if it has one, and, for an
 * entry that opens another list, "submenu", that list's index.  The
 * command menu's REPLY names a command by its list and its index there.
 */
static void net_query_commands(const struct net_json *v)
{
	struct net_jw w;
	int mode = (player && OPT(player, rogue_like_commands)) ?
		KEYMAP_MODE_ROGUE : KEYMAP_MODE_ORIG;
	size_t i, j;

	net_answer_begin(&w, "COMMANDS", v);
	net_jw_key(&w, "commands");
	net_jw_obj(&w);
	net_jw_kstr(&w, "keyset", mode == KEYMAP_MODE_ROGUE ? "roguelike" : "original");
	net_jw_key(&w, "lists");
	net_jw_arr(&w);
	for (i = 0; cmds_all[i].name; i++) {
		const struct command_list *l = &cmds_all[i];

		net_jw_obj(&w);
		net_jw_kstr(&w, "name", l->name);
		net_jw_kint(&w, "level", l->menu_level);
		net_jw_key(&w, "commands");
		net_jw_arr(&w);
		for (j = 0; j < l->len; j++) {
			const struct cmd_info *c = &l->list[j];
			struct keypress kp = { EVT_KBRD, c->key[mode], 0 };

			net_jw_obj(&w);
			/* With the native screen, / opens the knowledge of monsters */
			net_jw_kstr(&w, "name", streq(c->desc, "Identify symbol")
				&& screen_claimed[SCREEN_KNOWLEDGE]
				? "Monster knowledge" : c->desc);
			/* As cmd_sub_entry() shows it */
			if (kp.code) {
				char buf[16];

				keypress_to_readable(buf, sizeof buf, kp);
				net_jw_kstr(&w, "key", buf);
			}
			if (!c->cmd && !c->hook && c->nested_name) {
				int n = (int)cmd_list_lookup_by_name(c->nested_name);

				if (n >= 0) net_jw_kint(&w, "submenu", n);
			}
			net_jw_obj_end(&w);
		}
		net_jw_arr_end(&w);
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	net_jw_obj_end(&w);
	net_json_end(&w);
}

/* What COMPARE writes as equip_cmp_describe() calls back */
struct net_compare {
	struct net_jw *w;
	term *t;
	int nprop, row;
	bool items_open;
	int *pvals;
};

static const char *compare_categories[] = {
	"resistances", "abilities", "hindrances", "modifiers", "stat_modifiers"
};
static const char *compare_qualities[] = {
	"artifact", "ego", "good", "average", "bad"
};

/* One row of property values drawn on the hidden term, as "text", a
 * letter a property, and "colors", an attribute each */
static void net_compare_cells(struct net_compare *c, int y)
{
	struct net_buf text;
	int x;

	net_buf_init(&text);
	for (x = 0; x < c->nprop; x++) {
		char ch[5];
		wchar_t wc = c->t->scr->c[y][x];

		net_utf8(wc ? wc : L' ', ch);
		net_buf_append(&text, ch, strlen(ch));
	}
	net_jw_key(c->w, "text");
	net_jw_strn(c->w, (const char *)text.data, text.len);
	net_jw_key(c->w, "colors");
	net_jw_arr(c->w);
	for (x = 0; x < c->nprop; x++) net_jw_int(c->w, c->t->scr->a[y][x]);
	net_jw_arr_end(c->w);
	net_buf_free(&text);
}

static void net_compare_prop(void *closure, int category, const char *code,
		const char *label)
{
	struct net_compare *c = closure;

	net_jw_obj(c->w);
	net_jw_kstr(c->w, "category", compare_categories[category]);
	net_jw_kstr(c->w, "code", code);
	net_jw_kstr(c->w, "name", label);
	net_jw_obj_end(c->w);
	c->nprop++;
}

/* The player's values come before the items; write them once all the
 * rows are drawn, so keep them */
static void net_compare_player(void *closure, const int *vals)
{
	struct net_compare *c = closure;

	c->pvals = mem_alloc(MAX(c->nprop, 1) * sizeof(*c->pvals));
	memcpy(c->pvals, vals, c->nprop * sizeof(*c->pvals));
}

static void net_compare_item(void *closure, const struct object *obj,
		char source, int quality, int slot, const int *vals)
{
	struct net_compare *c = closure;
	char src[2] = { source, '\0' };
	int i;

	if (!c->items_open) {
		net_jw_arr_end(c->w);
		net_jw_key(c->w, "items");
		net_jw_arr(c->w);
		c->items_open = true;
	}
	c->row++;
	net_jw_obj(c->w);
	net_object_fields(c->w, obj, "compare", c->row - 1);
	net_jw_kstr(c->w, "source", src);
	net_jw_kstr(c->w, "quality", compare_qualities[quality]);
	net_jw_kint(c->w, "slot", slot);
	net_compare_cells(c, c->row);
	net_jw_key(c->w, "values");
	net_jw_arr(c->w);
	for (i = 0; i < c->nprop; i++) net_jw_int(c->w, vals[i]);
	net_jw_arr_end(c->w);
	net_jw_obj_end(c->w);
}

/**
 * COMPARE gives the equipment comparison, as equip_cmp_display() in
 * ui-equip-cmp.c shows it, with every item from every source for the
 * client to filter: "props", each property column with its "category",
 * its two-letter "code", and its full "name"; "player", the values for
 * the character with their equipment, as "text" and "colors", a letter
 * and an attribute per property, as the game draws them, and "values",
 * with how the map shows the character as "char", "color", and "tile"; and "items", in
 * the game's order, each an object as the inventory has it, with a
 * {"compare": n} reference that RECALL takes, its "source" ('e' worn,
 * 'p' pack, 'f' floor, 'h' home, 's' store), its rough "quality", its
 * equipment "slot", its "text" and "colors", and its "values", a number
 * per property for filtering.  "compare" is null without a character.
 */
static void net_query_compare(const struct net_json *v)
{
	struct net_jw w;
	struct net_compare c;
	struct equip_cmp_visitor vis;
	term *old, t;
	int n;

	net_answer_begin(&w, "COMPARE", v);
	net_jw_key(&w, "compare");
	if (!player || !player->upkeep || !character_generated || !player->race
			|| (n = equip_cmp_count()) < 0) {
		net_jw_null(&w);
		net_json_end(&w);
		return;
	}
	memset(&c, 0, sizeof(c));
	c.w = &w;
	c.t = &t;
	vis.prop = net_compare_prop;
	vis.item = net_compare_item;
	vis.player = net_compare_player;
	vis.closure = &c;

	net_jw_obj(&w);
	net_jw_key(&w, "props");
	net_jw_arr(&w);
	net_offscreen_begin(&t, &old, n + 2);
	n = equip_cmp_describe(&vis);
	if (!c.items_open) {
		net_jw_arr_end(&w);
		net_jw_key(&w, "items");
		net_jw_arr(&w);
	}
	net_jw_arr_end(&w);
	if (n >= 0) {
		int i;

		net_jw_key(&w, "player");
		net_jw_obj(&w);
		net_write_player_glyph(&w);
		net_compare_cells(&c, 0);
		net_jw_key(&w, "values");
		net_jw_arr(&w);
		for (i = 0; i < c.nprop && c.pvals; i++) net_jw_int(&w, c.pvals[i]);
		net_jw_arr_end(&w);
		net_jw_obj_end(&w);
	}
	net_offscreen_end(&t, old);
	mem_free(c.pvals);
	net_jw_obj_end(&w);
	net_json_end(&w);
}

/**
 * One staircase's tile in MAP, as a lit grid shows it, left out if the tile
 * set gives it a letter.
 */
static void net_map_stair_tile(struct net_jw *w, const char *key, int feat)
{
	int a = feat_x_attr[LIGHTING_LIT][feat];
	int c = feat_x_char[LIGHTING_LIT][feat];

	if (!(a & 0x80) || !(c & 0x80)) return;
	net_jw_key(w, key);
	net_jw_obj(w);
	net_jw_kint(w, "row", a & 0x7F);
	net_jw_kint(w, "col", c & 0x7F);
	net_jw_obj_end(w);
}

/**
 * MAP gives the whole level as the player remembers it, one grid at a
 * time, as the map view draws it (map_info() and grid_data_as_text(), as
 * prt_map_aux() in ui-map.c uses them), so letters, colors, and tiles
 * match the game's, lighting included.  The answer's "level_map" has its
 * "width" and "height", the "depth", the player's grid as "player", and
 * "rows", one object per row: in text mode "text", a string of one
 * letter per grid, and "colors", a color per grid; with tiles "tiles",
 * four numbers per grid as in PICT (the tile's row and column, then the
 * terrain's).  "marks" lists the stairs and shop entrances the player
 * knows, each with "x", "y", "kind" ("up", "down", or "shop"), and a
 * shop's "name".  With tiles, "stairs" has the tiles of a lit "up" and
 * "down" staircase, known on this level or not.  It is refused before the
 * level exists, and in headless mode, which draws no map.
 */
static const char *net_query_map(const struct net_json *v)
{
	struct chunk *known = player ? player->cave : NULL;
	bool tiles = use_graphics != GRAPHICS_NONE;
	struct net_jw w;
	struct grid_data g;
	int a, ta, x, y;
	wchar_t c, tc;
	char ch[5];
	struct net_buf text;

	if (!known || !cave || !character_dungeon || net_headless) {
		return "no level to map";
	}
	net_answer_begin(&w, "MAP", v);
	net_jw_key(&w, "level_map");
	net_jw_obj(&w);
	net_jw_kint(&w, "width", cave->width);
	net_jw_kint(&w, "height", cave->height);
	net_jw_kint(&w, "depth", player->depth);
	net_jw_key(&w, "player");
	net_jw_obj(&w);
	net_jw_kint(&w, "x", player->grid.x);
	net_jw_kint(&w, "y", player->grid.y);
	net_jw_obj_end(&w);
	net_jw_key(&w, "rows");
	net_jw_arr(&w);
	net_buf_init(&text);
	for (y = 0; y < cave->height; y++) {
		net_jw_obj(&w);
		if (tiles) {
			net_jw_key(&w, "tiles");
			net_jw_arr(&w);
		} else {
			text.len = 0;
		}
		for (x = 0; x < cave->width; x++) {
			map_info(loc(x, y), &g);
			grid_data_as_text(&g, &a, &c, &ta, &tc);
			if (tiles) {
				net_jw_int(&w, a & 0x7F);
				net_jw_int(&w, c & 0x7F);
				net_jw_int(&w, ta & 0x7F);
				net_jw_int(&w, tc & 0x7F);
			} else {
				net_utf8(c ? c : L' ', ch);
				net_buf_append(&text, ch, strlen(ch));
			}
		}
		if (tiles) {
			net_jw_arr_end(&w);
		} else {
			net_jw_key(&w, "text");
			net_jw_strn(&w, (const char *)text.data, text.len);
			net_jw_key(&w, "colors");
			net_jw_arr(&w);
			for (x = 0; x < cave->width; x++) {
				map_info(loc(x, y), &g);
				grid_data_as_text(&g, &a, &c, &ta, &tc);
				net_jw_int(&w, a);
			}
			net_jw_arr_end(&w);
		}
		net_jw_obj_end(&w);
	}
	net_jw_arr_end(&w);
	net_buf_free(&text);

	net_jw_key(&w, "marks");
	net_jw_arr(&w);
	for (y = 0; y < cave->height; y++) {
		for (x = 0; x < cave->width; x++) {
			struct loc grid = loc(x, y);
			const char *kind = NULL;

			if (square(known, grid)->feat == FEAT_NONE) continue;
			if (square_isupstairs(known, grid)) kind = "up";
			else if (square_isdownstairs(known, grid)) kind = "down";
			else if (square_isshop(known, grid)) kind = "shop";
			if (!kind) continue;
			net_jw_obj(&w);
			net_jw_kint(&w, "x", x);
			net_jw_kint(&w, "y", y);
			net_jw_kstr(&w, "kind", kind);
			if (streq(kind, "shop")) {
				net_jw_kstr(&w, "name", f_info[square(known, grid)->feat].name);
			}
			net_jw_obj_end(&w);
		}
	}
	net_jw_arr_end(&w);
	if (tiles) {
		net_jw_key(&w, "stairs");
		net_jw_obj(&w);
		net_map_stair_tile(&w, "up", FEAT_LESS);
		net_map_stair_tile(&w, "down", FEAT_MORE);
		net_jw_obj_end(&w);
	}
	net_jw_obj_end(&w);
	net_json_end(&w);
	return NULL;
}

/**
 * GLOSSARY gives every term the game's screens name, for the client's
 * descriptions on click and hover: "terms", each with its "id", such as
 * "ability:HOLD_LIFE" or "race:Hobbit", "kind", "title", "text" as styled
 * text, "manual" when the text is the manual's rather than a data file's
 * one line, and "names", each a "name" a screen may show it by and
 * "label" for a common word to mark only in labels; and "series", the
 * lead phrases item descriptions put before a list of terms, each with
 * its "items".  net-glossary.c builds it.
 */
static void net_query_glossary(const struct net_json *v)
{
	struct net_jw w;

	net_answer_begin(&w, "GLOSSARY", v);
	net_glossary_write(&w);
	net_json_end(&w);
}

static const char *net_query_visuals(const struct net_json *v);

/**
 * A heading of the VISIBLE list, as the classic lists word it.
 */
static void net_visible_heading(struct net_jw *w, const char *prefix, int n,
		bool others, const char *noun)
{
	char text[80];

	strnfmt(text, sizeof(text), "%s %d %s%s%s:", prefix, n,
		others ? "other " : "", noun, PLURAL(n));
	net_jw_obj(w);
	net_jw_kstr(w, "heading", text);
	net_jw_obj_end(w);
}

/**
 * A row of the VISIBLE list with nothing but text, in a color.
 */
static void net_visible_note(struct net_jw *w, const char *text, int color)
{
	net_jw_obj(w);
	net_jw_kstr(w, "name", text);
	net_jw_kint(w, "line", color);
	net_jw_obj_end(w);
}

/**
 * A thing's row of the VISIBLE list: how it looks, its name, where it is
 * if that is one place, and the color of its line.
 */
static void net_visible_row(struct net_jw *w, const struct knowledge_entry *e,
		const char *name, int dy, int dx, bool where, int line)
{
	net_jw_obj(w);
	net_write_entry_glyph(w, e);
	net_jw_kstr(w, "name", name);
	if (where) {
		char at[40];

		strnfmt(at, sizeof(at), "%d %s %d %s", abs(dy), dy <= 0 ? "N" : "S",
			abs(dx), dx <= 0 ? "W" : "E");
		net_jw_kstr(w, "where", at);
	}
	net_jw_kint(w, "line", line);
	net_jw_obj_end(w);
}

/**
 * The monsters of the VISIBLE list, as monster_list_format_section() in
 * ui-mon-list.c lays them out.
 */
static void net_visible_monsters(struct net_jw *w, bool by_exp)
{
	monster_list_t *list;
	int section;

	if (player->timed[TMD_IMAGE] > 0) {
		net_visible_note(w, "Your hallucinations are too wild to see things clearly.",
			COLOUR_ORANGE);
		return;
	}

	list = monster_list_new();
	monster_list_collect(list);
	monster_list_get_glyphs(list);
	monster_list_sort(list, by_exp ? monster_list_compare_exp
		: monster_list_standard_compare);

	if (!list->total_monsters[MONSTER_LIST_SECTION_LOS]
			&& !list->total_monsters[MONSTER_LIST_SECTION_ESP]) {
		net_visible_note(w, "You can see no monsters.", COLOUR_WHITE);
	}
	for (section = 0; section < MONSTER_LIST_SECTION_MAX; section++) {
		bool los = section == MONSTER_LIST_SECTION_LOS;
		int i;

		if (!list->total_monsters[section]) continue;
		net_visible_heading(w, los ? "You can see" : "You are aware of",
			list->total_monsters[section],
			!los && list->total_monsters[MONSTER_LIST_SECTION_LOS] > 0,
			"monster");
		for (i = 0; i < (int)list->distinct_entries; i++) {
			const monster_list_entry_t *m = &list->entries[i];
			struct knowledge_entry e;
			char name[120];
			int count = m->count[section], asleep = m->asleep[section];

			if (!count) continue;
			memset(&e, 0, sizeof(e));
			e.glyph = true;
			e.attr = m->attr;
			e.ch = monster_x_char[m->race->ridx];
			e.d_attr = m->race->d_attr;
			e.d_char = m->race->d_char;
			get_mon_name(name, sizeof(name), m->race, count);
			if (asleep > 0 && count > 1) {
				my_strcat(name, format(" (%d asleep)", asleep), sizeof(name));
			} else if (asleep == 1) {
				my_strcat(name, " (asleep)", sizeof(name));
			}
			net_visible_row(w, &e, name, m->dy[section], m->dx[section],
				count == 1, monster_list_entry_line_color(m));
		}
	}
	monster_list_free(list);
}

/**
 * The objects of the VISIBLE list, as object_list_format_section() in
 * ui-obj-list.c lays them out.
 */
static void net_visible_objects(struct net_jw *w)
{
	object_list_t *list = object_list_new();
	int section;

	object_list_collect(list);
	object_list_sort(list, object_list_standard_compare);

	if (!list->total_entries[OBJECT_LIST_SECTION_LOS]
			&& !list->total_entries[OBJECT_LIST_SECTION_NO_LOS]) {
		net_visible_note(w, "You can see no objects.", COLOUR_WHITE);
	}
	for (section = 0; section < OBJECT_LIST_SECTION_MAX; section++) {
		bool los = section == OBJECT_LIST_SECTION_LOS;
		int i;

		if (!list->total_entries[section]) continue;
		net_visible_heading(w, los ? "You can see" : "You are aware of",
			list->total_entries[section],
			!los && list->total_objects[OBJECT_LIST_SECTION_LOS] > 0,
			"object");
		for (i = 0; i < (int)list->distinct_entries; i++) {
			const object_list_entry_t *o = &list->entries[i];
			struct knowledge_entry e;
			char name[120];

			if (!o->count[section]) continue;
			memset(&e, 0, sizeof(e));
			e.glyph = true;
			e.attr = e.d_attr = COLOUR_RED;
			e.ch = e.d_char = L'*';
			if (!is_unknown(o->object) && o->object->kind) {
				const struct object_kind *kind = o->object->kind;
				bool aware = !kind->flavor || kind->aware;

				e.attr = object_kind_attr(kind);
				e.ch = object_kind_char(kind);
				e.d_attr = aware ? kind->d_attr : kind->flavor->d_attr;
				e.d_char = aware ? kind->d_char : kind->flavor->d_char;
			}
			object_list_format_name(o, name, sizeof(name));
			net_visible_row(w, &e, name, o->dy, o->dx, true,
				object_list_entry_line_attribute(o));
		}
	}
	object_list_free(list);
}

/**
 * VISIBLE is what the monster or item list shows now, for args "kind",
 * "monsters" or "items", with "by_exp" true to sort the monsters by the
 * experience they give.  "visible" has a row each in the list's order: a
 * "heading"; or a "name", with how the thing looks as "char", "color", and
 * with tiles "tile", where it is as "where", and its line's color as "line".
 */
static void net_query_visible(const struct net_json *v)
{
	struct net_jw w;
	const struct net_json *args = net_json_get(v, "args");
	const char *kind = NULL;
	bool by_exp = false;

	if (args) {
		kind = net_json_get_str(args, "kind");
		net_json_get_bool(args, "by_exp", &by_exp);
	}
	net_answer_begin(&w, "VISIBLE", v);
	net_jw_key(&w, "visible");
	net_jw_arr(&w);
	if (character_dungeon && kind) {
		if (streq(kind, "monsters")) {
			net_visible_monsters(&w, by_exp);
		} else if (streq(kind, "items")) {
			net_visible_objects(&w);
		}
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
}

static const struct {
	const char *name;
	void (*answer)(const struct net_json *v);
} queries[] = {
	{ "SNAPSHOT", net_query_snapshot },
	{ "SAVEFILES", net_query_savefiles },
	{ "OPTIONS", net_query_options },
	{ "RECALL", net_query_recall },
	{ "INVENTORY", net_query_inventory },
	{ "SPELLS", net_query_spells },
	{ "STORE", net_query_store },
	{ "BIRTH", net_query_birth },
	{ "LOOK", net_query_look },
	{ "MONSTER_LIST", net_query_monster_list },
	{ "VISIBLE", net_query_visible },
	{ "KNOWLEDGE", net_query_knowledge },
	{ "CHARACTER", net_query_character },
	{ "MESSAGES", net_query_messages },
	{ "HELP", net_query_help },
	{ "TOMB", net_query_tomb },
	{ "COMPARE", net_query_compare },
	{ "COMMANDS", net_query_commands },
	{ "GLOSSARY", net_query_glossary },
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
 * Visuals, for the Tile Editor
 *
 * VISUALS lists the tile each monster, object kind, flavor, terrain
 * feature, trap, and spell effect uses.  SET_VISUAL changes one at once.
 * The player's changes to a tile set live in the user folder, in
 * tiles/<set>.prf, which the engine reads after the set's own files each
 * time it loads them.  SAVE_VISUALS writes that file, and RELOAD_VISUALS
 * drops the changes made since.
 *
 * Changes come in layers, each over the ones before it: the defaults
 * Fancyband ships, in lib/tiles/fancyband; the player's own for every
 * character, in the user folder; and one character's, in
 * characters/<savefile> in the user folder.  Each layer has its own
 * files, and the map shows the topmost change for each tile.  The Tile
 * Editor's commands name the layer they work on, "global" if not.
 *
 * The engine keeps the set's own tiles and the player's changes beside
 * the game's tables, one slot for each tile: one for a monster, object,
 * or flavor, one for each kind of light for terrain and traps, and five
 * for an effect, a still one and one for each bolt direction.
 *
 * SET_SYMBOL changes the letter and color an entry has when the map
 * draws letters.  Those changes work with every tile set, so they live in
 * symbols.prf in the user folder, which the engine reads into its own
 * table, one slot for each entry, and puts into the game's tables each
 * time it loads the letters.  SAVE_VISUALS and RELOAD_VISUALS cover them
 * too.
 * ------------------------------------------------------------------------ */

enum vis_kind {
	VIS_MONSTER, VIS_OBJECT, VIS_FLAVOR, VIS_TERRAIN, VIS_TRAP, VIS_EFFECT,
	VIS_PLAYER, VIS_MAX
};

static const char *vis_kind_names[VIS_MAX] = {
	"monster", "object", "flavor", "terrain", "trap", "effect", "player"
};

/* The kinds of light, in LIGHTING_ order, as the pref files name them */
static const char *vis_lights[LIGHTING_MAX] = { "los", "torch", "lit", "dark" };

/* The bolt directions, in BOLT_ order, as the pref files name them */
static const char *vis_directions[BOLT_MAX] = { "static", "0", "45", "90", "135" };

/* The slots: where each kind starts, how many entries it has, and how
 * many tiles each entry has */
static size_t vis_first[VIS_MAX + 1];
static int vis_count[VIS_MAX];
static int vis_tiles[VIS_MAX] = { 1, 1, 1, LIGHTING_MAX, LIGHTING_MAX, BOLT_MAX, 1 };

/*
 * The player's tiles.  The tile sets give the player a tile with
 * "monster:<player>" lines, some of them only for one race and class,
 * behind a "?:" line such as "?:[AND [EQU $CLASS Warrior] [EQU $RACE Elf] ]".
 * The game reads only the lines for the character's own race and class,
 * so the engine keeps its own table: entry 0 is <player>, the tile for a
 * race and class with none of its own, and entry 1 + class * races + race
 * is that race and class's own tile, or 0 for none.  The last entry is
 * the character's own tile, or 0 for none.  The map's tile for the
 * player, monster 0's, is the character's own if they have one, else
 * their race and class's own if it has one, else <player>'s.
 *
 * A character's own tile is kept in the character's layer, behind a
 * condition of Fancyband's own on the character's savefile, which the
 * game has no variable for: "?:[EQU $SAVEFILE name]".  The engine reads
 * those lines itself.
 */
static uint8_t *plr_a;
static wchar_t *plr_c;
static int plr_races, plr_classes;

/* The layers, lowest first */
enum { LAYER_DEFAULTS, LAYER_GLOBAL, LAYER_CHARACTER, LAYER_MAX };
static const char *vis_layer_names[LAYER_MAX] = { "defaults", "global", "character" };

/* Where the defaults are read from and written to instead of
 * lib/tiles/fancyband: the source tree's, in developer mode */
char net_defaults_dir[1024];

/* What a slot holds before a file of changes is read into the game's
 * tables, so the engine can tell which slots the file changed.  The
 * engine refuses row 127, so no tile has this attribute. */
#define VIS_NONE_A 0xFF
#define VIS_NONE_C ((wchar_t)0xFFFF)

/* The entry for the character's own tile */
static int plr_you(void)
{
	return vis_count[VIS_PLAYER] - 1;
}

/* The character's savefile's name, which their own tile is kept by, or
 * NULL before there is one */
static const char *plr_savefile(void)
{
	const char *name = strrchr(savefile, PATH_SEPC);

	name = name ? name + 1 : savefile;
	return name[0] && character_generated ? name : NULL;
}

/* The set's own tiles, and each layer's changes where its has is set */
static uint8_t *vis_own_a;
static wchar_t *vis_own_c;
static bool *lay_has[LAYER_MAX];
static uint8_t *lay_a[LAYER_MAX];
static wchar_t *lay_c[LAYER_MAX];

/* The set the changes belong to, the character whose layer was read,
 * and whether to read the files again */
static char vis_set[64];
static char vis_who[128];
static bool vis_reread = true;

/* Each layer's symbols, one slot for each entry, where its has is set;
 * whether to read the symbols files again; and the effects' own colors,
 * which text mode draws them in */
static size_t sym_first[VIS_MAX + 1];
static bool *sym_has[LAYER_MAX];
static uint8_t *sym_a[LAYER_MAX];
static wchar_t *sym_c[LAYER_MAX];
static bool sym_reread = true;
static uint8_t *proj_own_color;

static struct flavor *vis_flavor(int fidx)
{
	struct flavor *f;

	for (f = flavors; f; f = f->next) {
		if ((int)f->fidx == fidx) return f;
	}
	return NULL;
}

/* Lay out the slots once the game data has loaded */
static bool vis_layout(void)
{
	struct flavor *f;
	size_t n = 0;
	int k, l;

	if (vis_own_a) return true;
	if (!z_info || !monster_x_attr) return false;
	vis_count[VIS_MONSTER] = z_info->r_max;
	vis_count[VIS_OBJECT] = z_info->k_max;
	vis_count[VIS_FLAVOR] = 0;
	for (f = flavors; f; f = f->next) {
		vis_count[VIS_FLAVOR] = MAX(vis_count[VIS_FLAVOR], (int)f->fidx + 1);
	}
	vis_count[VIS_TERRAIN] = FEAT_MAX;
	vis_count[VIS_TRAP] = z_info->trap_max;
	vis_count[VIS_EFFECT] = PROJ_MAX;
	plr_races = plr_classes = 0;
	{
		struct player_race *r;
		struct player_class *cl;

		for (r = races; r; r = r->next) {
			plr_races = MAX(plr_races, (int)r->ridx + 1);
		}
		for (cl = classes; cl; cl = cl->next) {
			plr_classes = MAX(plr_classes, (int)cl->cidx + 1);
		}
	}
	vis_count[VIS_PLAYER] = 2 + plr_races * plr_classes;
	plr_a = mem_zalloc(vis_count[VIS_PLAYER] * sizeof(uint8_t));
	plr_c = mem_zalloc(vis_count[VIS_PLAYER] * sizeof(wchar_t));
	for (k = 0; k < VIS_MAX; k++) {
		vis_first[k] = n;
		n += (size_t)vis_count[k] * vis_tiles[k];
	}
	vis_first[VIS_MAX] = n;
	vis_own_a = mem_zalloc(n * sizeof(uint8_t));
	vis_own_c = mem_zalloc(n * sizeof(wchar_t));
	for (l = 0; l < LAYER_MAX; l++) {
		lay_has[l] = mem_zalloc(n * sizeof(bool));
		lay_a[l] = mem_zalloc(n * sizeof(uint8_t));
		lay_c[l] = mem_zalloc(n * sizeof(wchar_t));
	}

	n = 0;
	for (k = 0; k < VIS_MAX; k++) {
		sym_first[k] = n;
		n += (size_t)vis_count[k];
	}
	sym_first[VIS_MAX] = n;
	for (l = 0; l < LAYER_MAX; l++) {
		sym_has[l] = mem_zalloc(n * sizeof(bool));
		sym_a[l] = mem_zalloc(n * sizeof(uint8_t));
		sym_c[l] = mem_zalloc(n * sizeof(wchar_t));
	}
	proj_own_color = mem_zalloc(PROJ_MAX * sizeof(uint8_t));
	for (k = 0; k < PROJ_MAX; k++) {
		if (projections) proj_own_color[k] = projections[k].color;
	}
	return true;
}

/* The race and class of a player entry past <player> */
static bool plr_race_class(int id, const struct player_race **r,
		const struct player_class **cl)
{
	if (id < 1 || id >= 1 + plr_races * plr_classes) return false;
	*r = player_id2race((id - 1) % plr_races);
	*cl = player_id2class((id - 1) / plr_races);
	return *r && *cl;
}

/* The player entry for a race and class */
static int plr_entry(const struct player_race *r, const struct player_class *cl)
{
	return 1 + (int)cl->cidx * plr_races + (int)r->ridx;
}

/* Whether an entry exists and can take a tile */
static bool vis_valid(int kind, int id)
{
	if (kind < 0 || kind >= VIS_MAX || id < 0 || id >= vis_count[kind]) {
		return false;
	}
	switch (kind) {
		case VIS_MONSTER: return r_info[id].name != NULL;
		case VIS_OBJECT: return k_info[id].name && k_info[id].tval;
		case VIS_FLAVOR: return vis_flavor(id) != NULL;
		case VIS_TERRAIN: return f_info[id].name && !f_info[id].mimic;
		case VIS_TRAP: return id > 0 && trap_info[id].name && trap_info[id].desc;
		case VIS_PLAYER: {
			const struct player_race *r;
			const struct player_class *cl;

			if (id == plr_you()) return plr_savefile() != NULL;
			return id == 0 || plr_race_class(id, &r, &cl);
		}
		default: return projections && projections[id].name;
	}
}

/* The game's table entry for one tile of an entry */
static void vis_table(int kind, int id, int i, uint8_t **a, wchar_t **c)
{
	switch (kind) {
		case VIS_MONSTER: *a = &monster_x_attr[id]; *c = &monster_x_char[id]; break;
		case VIS_OBJECT: *a = &kind_x_attr[id]; *c = &kind_x_char[id]; break;
		case VIS_FLAVOR: *a = &flavor_x_attr[id]; *c = &flavor_x_char[id]; break;
		case VIS_TERRAIN: *a = &feat_x_attr[i][id]; *c = &feat_x_char[i][id]; break;
		case VIS_TRAP: *a = &trap_x_attr[i][id]; *c = &trap_x_char[i][id]; break;
		case VIS_PLAYER: *a = &plr_a[id]; *c = &plr_c[id]; break;
		default: *a = &proj_to_attr[id][i]; *c = &proj_to_char[id][i]; break;
	}
}

static size_t vis_slot(int kind, int id, int i)
{
	return vis_first[kind] + (size_t)id * vis_tiles[kind] + i;
}

/* The name of the tile set in use, for its file of changes.  Sets that
 * share a folder, as Shockbolt's dark and light ones do, add the end of
 * their pref file's name. */
static bool vis_set_name(char *buf, size_t len)
{
	graphics_mode *mode = use_graphics ? get_graphics_mode(use_graphics) : NULL;
	graphics_mode *m;
	char dir[256], pref[32];
	char *end, *base, *dash;
	bool shared = false;

	if (!mode || mode->grafID == GRAPHICS_NONE) return false;
	my_strcpy(dir, mode->path, sizeof(dir));
	end = dir + strlen(dir);
	while (end > dir && (end[-1] == '/' || end[-1] == '\\')) *--end = 0;
	base = MAX(strrchr(dir, '/'), strrchr(dir, '\\'));
	base = base ? base + 1 : dir;
	for (m = graphics_modes; m; m = m->pNext) {
		if (m != mode && streq(m->path, mode->path)) shared = true;
	}
	if (!shared) {
		my_strcpy(buf, base, len);
		return true;
	}
	my_strcpy(pref, mode->pref, sizeof(pref));
	if (suffix(pref, ".prf")) pref[strlen(pref) - 4] = 0;
	dash = strrchr(pref, '-');
	strnfmt(buf, len, "%s-%s", base, dash ? dash + 1 : pref);
	return true;
}

/* The folder that holds a layer's files, or false for the character's
 * layer before there is a character */
static bool lay_dir(int layer, char *buf, size_t len)
{
	char dir[1024];
	const char *me;

	switch (layer) {
		case LAYER_DEFAULTS:
			if (net_defaults_dir[0]) {
				my_strcpy(buf, net_defaults_dir, len);
			} else {
				/* ANGBAND_DIR_TILES can be relative to this program's
				 * folder, but the client reads the filters beside these
				 * files from its own, so the path must be absolute */
				char *abs_dir;

				path_build(buf, len, ANGBAND_DIR_TILES, "fancyband");
				abs_dir = realpath(buf, NULL);
				if (abs_dir) {
					my_strcpy(buf, abs_dir, len);
					free(abs_dir);
				}
			}
			return true;
		case LAYER_GLOBAL:
			my_strcpy(buf, ANGBAND_DIR_USER, len);
			return true;
		default:
			me = plr_savefile();
			if (!me) return false;
			path_build(dir, sizeof(dir), ANGBAND_DIR_USER, "characters");
			path_build(buf, len, dir, me);
			return true;
	}
}

/* A layer's file of changes for a tile set: <set>.prf among the
 * defaults, tiles/<set>.prf in the player's folders */
static bool vis_file(int layer, char *buf, size_t len, const char *set)
{
	char dir[1024], sub[1024];

	if (!lay_dir(layer, dir, sizeof(dir))) return false;
	if (layer == LAYER_DEFAULTS) {
		path_build(buf, len, dir, format("%s.prf", set));
	} else {
		path_build(sub, sizeof(sub), dir, "tiles");
		path_build(buf, len, sub, format("%s.prf", set));
	}
	return true;
}

static void vis_on_tiles(void);
static void vis_describe(int kind, int id, char *name, size_t len,
		wchar_t *ch, int *color);

/* A layer's symbols, which work with every tile set */
static bool sym_file(int layer, char *buf, size_t len)
{
	char dir[1024];

	if (!lay_dir(layer, dir, sizeof(dir))) return false;
	path_build(buf, len, dir, "symbols.prf");
	return true;
}

static size_t sym_slot(int kind, int id)
{
	return sym_first[kind] + (size_t)id;
}

/* An entry's own letter and color, as the game data gives them */
static void sym_own(int kind, int id, uint8_t *a, wchar_t *c)
{
	char name[120];
	int color;

	vis_describe(kind, id, name, sizeof(name), c, &color);
	*a = (uint8_t)color;
}

/* The letter and color an entry has as the layers up to top give them,
 * and the layer that gives them, or -1 for the game's own */
static int sym_at(int top, int kind, int id, uint8_t *a, wchar_t *c)
{
	size_t s = sym_slot(kind, id);
	int l;

	for (l = top; l >= 0; l--) {
		if (sym_has[l][s]) {
			*a = sym_a[l][s];
			*c = sym_c[l][s];
			return l;
		}
	}
	sym_own(kind, id, a, c);
	return -1;
}

/* The letter and color an entry has when the map draws letters */
static void sym_current(int kind, int id, uint8_t *a, wchar_t *c)
{
	sym_at(LAYER_MAX - 1, kind, id, a, c);
}

/**
 * Read each layer's symbols.prf.  The pref lines write to the game's
 * tables, so those are kept, set to a marker before each file, read back
 * after it, and then put back as they were, tiles or letters.
 */
static void sym_load(void)
{
	size_t n = vis_first[VIS_MAX];
	uint8_t *keep_a = mem_zalloc(n * sizeof(uint8_t));
	wchar_t *keep_c = mem_zalloc(n * sizeof(wchar_t));
	char path[1024], line[1024];
	ang_file *f;
	int k, id, i, l;

	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			if (k == VIS_PLAYER || !vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);
				uint8_t *a;
				wchar_t *c;

				vis_table(k, id, i, &a, &c);
				keep_a[s] = *a;
				keep_c[s] = *c;
			}
		}
	}
	for (l = 0; l < LAYER_MAX; l++) {
		memset(sym_has[l], 0, sym_first[VIS_MAX] * sizeof(bool));
		if (!sym_file(l, path, sizeof(path)) || !file_exists(path)) continue;
		f = file_open(path, MODE_READ, FTYPE_TEXT);
		if (!f) continue;
		for (k = 0; k < VIS_MAX; k++) {
			for (id = 0; id < vis_count[k]; id++) {
				if (k == VIS_PLAYER || !vis_valid(k, id)) continue;
				for (i = 0; i < vis_tiles[k]; i++) {
					uint8_t *a;
					wchar_t *c;

					vis_table(k, id, i, &a, &c);
					*a = VIS_NONE_A;
					*c = VIS_NONE_C;
				}
			}
		}
		while (file_getl(f, line, sizeof(line))) {
			if (line[0] && line[0] != '#' && !prefix(line, "?:")) {
				process_pref_file_command(line);
			}
		}
		file_close(f);
		for (k = 0; k < VIS_MAX; k++) {
			for (id = 0; id < vis_count[k]; id++) {
				size_t t = sym_slot(k, id);
				uint8_t own_a, *a;
				wchar_t own_c, *c;

				if (k == VIS_PLAYER || !vis_valid(k, id)) continue;
				/* The first slot: every line of an entry gives the same */
				vis_table(k, id, 0, &a, &c);
				if (*a == VIS_NONE_A && *c == VIS_NONE_C) continue;
				sym_own(k, id, &own_a, &own_c);
				sym_has[l][t] = true;
				sym_a[l][t] = *a;
				sym_c[l][t] = k == VIS_EFFECT ? own_c : *c;
			}
		}
	}
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			if (k == VIS_PLAYER || !vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);
				uint8_t *a;
				wchar_t *c;

				vis_table(k, id, i, &a, &c);
				*a = keep_a[s];
				*c = keep_c[s];
			}
		}
	}
	mem_free(keep_a);
	mem_free(keep_c);
}

/**
 * Put one entry's symbol into the game's tables, when the map draws
 * letters.  Text mode draws an effect in its own color, as * or a line
 * along its path, so an effect takes only the color.
 */
static void sym_apply_one(int kind, int id)
{
	uint8_t sa, *a;
	wchar_t sc, *c;
	int i;

	sym_current(kind, id, &sa, &sc);
	if (kind == VIS_EFFECT) {
		projections[id].color = sa;
		return;
	}
	for (i = 0; i < vis_tiles[kind]; i++) {
		vis_table(kind, id, i, &a, &c);
		*a = sa;
		*c = sc;
	}
}

/* Put the player's symbols into the game's tables, or, with tiles, give
 * the effects back their own colors */
static void sym_apply(void)
{
	int k, id;

	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			uint8_t a;
			wchar_t c;

			if (k == VIS_PLAYER || !vis_valid(k, id)) continue;
			if (use_graphics == GRAPHICS_NONE) {
				if (sym_at(LAYER_MAX - 1, k, id, &a, &c) >= 0) sym_apply_one(k, id);
				else if (k == VIS_EFFECT) projections[id].color = proj_own_color[id];
			} else if (k == VIS_EFFECT) {
				projections[id].color = proj_own_color[id];
			}
		}
	}
}

/**
 * After reset_visuals() loads the letters or a tile set's files: put in
 * the player's symbols, reading symbols.prf when asked to; then, with a
 * tile set, its tiles.
 */
static void net_on_visuals(void)
{
	const char *me = plr_savefile();

	if (!vis_layout()) return;
	/* A new character has a layer of their own to read */
	if (!streq(me ? me : "", vis_who)) {
		my_strcpy(vis_who, me ? me : "", sizeof(vis_who));
		vis_reread = sym_reread = true;
	}
	if (sym_reread) {
		sym_load();
		sym_reread = false;
	}
	sym_apply();
	if (use_graphics != GRAPHICS_NONE) vis_on_tiles();
}

/*
 * The lines of a tile set's files that give the player a tile: "?:"
 * conditions, "%:" files to include, and "monster:<player>" tiles, file
 * by file, so the engine can work out every race and class's tile
 * without loading the whole set for each.
 */
struct plr_line {
	char type;		/* '?', '%', or 'm' */
	char *expr;		/* a condition */
	int file;		/* a file to include, by its index */
	uint8_t a;		/* a tile */
	wchar_t c;
};

struct plr_file {
	struct plr_line *lines;
	int n, cap;
};

#define PLR_FILES_MAX 16
static struct plr_file plr_files[PLR_FILES_MAX];
static int plr_nfiles;

static void plr_free_files(void)
{
	int i, j;

	for (i = 0; i < plr_nfiles; i++) {
		for (j = 0; j < plr_files[i].n; j++) {
			string_free(plr_files[i].lines[j].expr);
		}
		mem_free(plr_files[i].lines);
	}
	memset(plr_files, 0, sizeof(plr_files));
	plr_nfiles = 0;
}

/* Read one file, from the set's folder, or from lib/customize as the game
 * would find it, and the files it includes.  Returns its index or -1. */
static int plr_load(const char *dir, const char *name, int depth)
{
	char path[1024], line[1024];
	ang_file *f;
	int idx;

	if (depth > 8 || plr_nfiles >= PLR_FILES_MAX) return -1;
	path_build(path, sizeof(path), dir, name);
	if (!file_exists(path)) path_build(path, sizeof(path), ANGBAND_DIR_CUSTOMIZE, name);
	f = file_open(path, MODE_READ, FTYPE_TEXT);
	if (!f) return -1;
	idx = plr_nfiles++;
	while (file_getl(f, line, sizeof(line))) {
		struct plr_file *pf;
		struct plr_line l;
		size_t n = strlen(line);
		unsigned a, ch;

		while (n > 0 && isspace((unsigned char)line[n - 1])) line[--n] = 0;
		memset(&l, 0, sizeof(l));
		if (prefix(line, "?:")) {
			l.type = '?';
			l.expr = string_make(line + 2);
		} else if (prefix(line, "%:")) {
			l.type = '%';
			l.file = plr_load(dir, line + 2, depth + 1);
			if (l.file < 0) continue;
		} else if (prefix(line, "monster:<player>:")
				&& sscanf(line + 17, "%i:%i", (int *)&a, (int *)&ch) == 2) {
			l.type = 'm';
			l.a = (uint8_t)a;
			l.c = (wchar_t)ch;
		} else {
			continue;
		}
		pf = &plr_files[idx];
		if (pf->n == pf->cap) {
			pf->cap = pf->cap ? pf->cap * 2 : 32;
			pf->lines = mem_realloc(pf->lines, pf->cap * sizeof(*pf->lines));
		}
		pf->lines[pf->n++] = l;
	}
	file_close(f);
	return idx;
}

/*
 * Go through a file as the game would for the player as they are now:
 * a "?:" line that is false skips the lines after it, up to the next
 * "?:" line, in that file.  A tile behind a condition on $RACE or $CLASS
 * is the race and class's own; any other is <player>'s.
 */
static void plr_eval(int file, bool in_cond, int depth, int *def, int *own)
{
	bool bypass = false, cond = in_cond;
	int i;

	if (file < 0 || depth > 8) return;
	for (i = 0; i < plr_files[file].n; i++) {
		const struct plr_line *l = &plr_files[file].lines[i];

		if (l->type == '?') {
			bypass = !prefs_expr_true(l->expr);
			cond = in_cond || strstr(l->expr, "$RACE") || strstr(l->expr, "$CLASS");
		} else if (bypass) {
			continue;
		} else if (l->type == '%') {
			plr_eval(l->file, cond, depth + 1, def, own);
		} else if (cond) {
			*own = (int)((file << 16) | i);
		} else {
			*def = (int)((file << 16) | i);
		}
	}
}

static const struct plr_line *plr_line_at(int at)
{
	return &plr_files[at >> 16].lines[at & 0xFFFF];
}

/* Put the set's own player tiles into the player's table: <player>'s,
 * and each race and class's, or 0 for one with none of its own */
static void plr_scan(void)
{
	graphics_mode *mode = get_graphics_mode(use_graphics);
	const struct player_race *keep_r = player->race;
	const struct player_class *keep_c = player->class;
	int root, id;

	plr_free_files();
	memset(plr_a, 0, vis_count[VIS_PLAYER] * sizeof(uint8_t));
	memset(plr_c, 0, vis_count[VIS_PLAYER] * sizeof(wchar_t));
	if (!mode) return;
	root = plr_load(mode->path, mode->pref, 0);
	if (root < 0) return;
	for (id = 1; id < vis_count[VIS_PLAYER]; id++) {
		int def = -1, own = -1;

		if (!plr_race_class(id, &player->race, &player->class)) continue;
		plr_eval(root, false, 0, &def, &own);
		if (def >= 0 && !plr_a[0]) {
			plr_a[0] = plr_line_at(def)->a;
			plr_c[0] = plr_line_at(def)->c;
		}
		if (own >= 0) {
			plr_a[id] = plr_line_at(own)->a;
			plr_c[id] = plr_line_at(own)->c;
		}
	}
	player->race = keep_r;
	player->class = keep_c;
	plr_free_files();
}

/* Give the map the character's tile: their race and class's own, or
 * else <player>'s */
static void plr_resolve(void)
{
	int id = 0;

	if (use_graphics == GRAPHICS_NONE || !plr_a) return;
	if (player && player->race && player->class) {
		id = plr_entry(player->race, player->class);
	}
	if (plr_savefile() && (plr_a[plr_you()] & 0x80)) {
		monster_x_attr[0] = plr_a[plr_you()];
		monster_x_char[0] = plr_c[plr_you()];
	} else if (id > 0 && id < plr_you() && (plr_a[id] & 0x80)) {
		monster_x_attr[0] = plr_a[id];
		monster_x_char[0] = plr_c[id];
	} else if (plr_a[0] || plr_c[0]) {
		monster_x_attr[0] = plr_a[0];
		monster_x_char[0] = plr_c[0];
	}
}

/* The player entry the lines after a "?:" line in the file of changes
 * are for: 0, <player>, after "?:1", a race and class after the line
 * SAVE_VISUALS writes for them, or -1 for any other */
static int plr_entry_of(const char *expr)
{
	char cname[64], rname[64];
	struct player_race *r;
	struct player_class *cl;
	size_t n;

	if (streq(expr, "1")) return 0;
	if (prefix(expr, "[EQU $SAVEFILE ")) return -2;
	if (sscanf(expr, "[AND [EQU $CLASS %63s [EQU $RACE %63s", cname, rname) != 2) {
		return -1;
	}
	for (n = strlen(cname); n > 0 && cname[n - 1] == ']'; ) cname[--n] = 0;
	for (n = strlen(rname); n > 0 && rname[n - 1] == ']'; ) rname[--n] = 0;
	for (r = races; r; r = r->next) {
		if (!streq(r->name, rname)) continue;
		for (cl = classes; cl; cl = cl->next) {
			if (streq(cl->name, cname)) return plr_entry(r, cl);
		}
	}
	return -1;
}

/**
 * Read one layer's file of changes for a tile set into its slots.  The
 * lines write to the game's tables, so every slot is set to a marker
 * first, and a slot the file changed no longer holds it.  The player's
 * tiles go to the player's table, by the race and class of the "?:" line
 * before them; in the character's layer, the lines behind their
 * savefile's condition are the character's own.  The caller puts the
 * tables right after.
 */
static void vis_read_layer(int layer, const char *set)
{
	char path[1024], line[1024];
	ang_file *f;
	int k, id, i, plr = 0;

	memset(lay_has[layer], 0, vis_first[VIS_MAX] * sizeof(bool));
	if (!vis_file(layer, path, sizeof(path), set) || !file_exists(path)) return;
	f = file_open(path, MODE_READ, FTYPE_TEXT);
	if (!f) return;
	for (k = 0; k < VIS_MAX; k++) {
		if (k == VIS_PLAYER) continue;
		for (id = 0; id < vis_count[k]; id++) {
			if (!vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				uint8_t *a;
				wchar_t *c;

				vis_table(k, id, i, &a, &c);
				*a = VIS_NONE_A;
				*c = VIS_NONE_C;
			}
		}
	}
	while (file_getl(f, line, sizeof(line))) {
		unsigned a, ch;

		if (!line[0] || line[0] == '#') continue;
		if (prefix(line, "?:")) {
			plr = plr_entry_of(line + 2);
		} else if (prefix(line, "monster:<player>:")) {
			int to = plr;
			size_t s;

			if (sscanf(line + 17, "%i:%i", (int *)&a, (int *)&ch) != 2) continue;
			if (plr == -2) to = layer == LAYER_CHARACTER && plr_savefile() ? plr_you() : -1;
			if (to < 0) continue;
			s = vis_slot(VIS_PLAYER, to, 0);
			lay_has[layer][s] = true;
			lay_a[layer][s] = (uint8_t)a;
			lay_c[layer][s] = (wchar_t)ch;
		} else {
			process_pref_file_command(line);
		}
	}
	file_close(f);
	for (k = 0; k < VIS_MAX; k++) {
		if (k == VIS_PLAYER) continue;
		for (id = 0; id < vis_count[k]; id++) {
			if (!vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);
				uint8_t *a;
				wchar_t *c;

				vis_table(k, id, i, &a, &c);
				if (*a == VIS_NONE_A && *c == VIS_NONE_C) continue;
				lay_has[layer][s] = true;
				lay_a[layer][s] = *a;
				lay_c[layer][s] = *c;
			}
		}
	}
}

/* The tile in slot s as the layers up to top give it, and the layer that
 * gives it, or -1 for the set's own */
static int vis_at(int top, size_t s, uint8_t *a, wchar_t *c)
{
	int l;

	for (l = top; l >= 0; l--) {
		if (lay_has[l][s]) {
			*a = lay_a[l][s];
			*c = lay_c[l][s];
			return l;
		}
	}
	*a = vis_own_a[s];
	*c = vis_own_c[s];
	return -1;
}

/* Put an entry's tiles, as every layer gives them, into the game's
 * tables */
static void vis_apply_one(int kind, int id)
{
	int i;

	for (i = 0; i < vis_tiles[kind]; i++) {
		uint8_t *a;
		wchar_t *c;

		vis_table(kind, id, i, &a, &c);
		vis_at(LAYER_MAX - 1, vis_slot(kind, id, i), a, c);
	}
}

/**
 * After reset_visuals() loads a tile set's files: keep the set's own
 * tiles, then put the layers' changes over them.  For a new set or a new
 * character, or after RELOAD_VISUALS, the changes come from the layers'
 * files; otherwise the changes made since stay.
 */
static void vis_on_tiles(void)
{
	char set[64];
	int k, id, i, l;

	if (!vis_set_name(set, sizeof(set))) return;
	plr_scan();
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			if (!vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);
				uint8_t *a;
				wchar_t *c;

				vis_table(k, id, i, &a, &c);
				vis_own_a[s] = *a;
				vis_own_c[s] = *c;
			}
		}
	}
	if (vis_reread || !streq(set, vis_set)) {
		my_strcpy(vis_set, set, sizeof(vis_set));
		vis_reread = false;
		for (l = 0; l < LAYER_MAX; l++) vis_read_layer(l, set);
	}
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			if (vis_valid(k, id)) vis_apply_one(k, id);
		}
	}
	plr_resolve();
}

/* Write a tile as [row, column] on the sheet, or null for a letter */
static void vis_jw_tile(struct net_jw *w, uint8_t a, wchar_t c)
{
	if (!(a & 0x80)) {
		net_jw_null(w);
		return;
	}
	net_jw_arr(w);
	net_jw_int(w, a & 0x7F);
	net_jw_int(w, c & 0x7F);
	net_jw_arr_end(w);
}

/* An entry's name, letter, and color, as the browser lists it */
static void vis_describe(int kind, int id, char *name, size_t len,
		wchar_t *ch, int *color)
{
	switch (kind) {
		case VIS_MONSTER:
			my_strcpy(name, r_info[id].name, len);
			*ch = r_info[id].d_char;
			*color = r_info[id].d_attr;
			break;
		case VIS_OBJECT:
			obj_desc_name_format(name, len, 0, k_info[id].name, NULL, false);
			*ch = k_info[id].d_char;
			*color = k_info[id].d_attr;
			break;
		case VIS_FLAVOR: {
			struct flavor *f = vis_flavor(id);

			strnfmt(name, len, "%s %s", f->text, tval_find_name(f->tval));
			*ch = f->d_char;
			*color = f->d_attr;
			break;
		}
		case VIS_TERRAIN:
			my_strcpy(name, f_info[id].name, len);
			*ch = f_info[id].d_char;
			*color = f_info[id].d_attr;
			break;
		case VIS_TRAP:
			my_strcpy(name, trap_info[id].desc, len);
			*ch = trap_info[id].d_char;
			*color = trap_info[id].d_attr;
			break;
		case VIS_PLAYER: {
			const struct player_race *r;
			const struct player_class *cl;

			/* Every race and class has <player>'s letter */
			if (plr_race_class(id, &r, &cl)) {
				strnfmt(name, len, "%s %s", r->name, cl->name);
			} else if (id == plr_you()) {
				my_strcpy(name, player->full_name, len);
			} else {
				my_strcpy(name, "Default for every race and class", len);
			}
			*ch = r_info[0].d_char;
			*color = r_info[0].d_attr;
			break;
		}
		default:
			my_strcpy(name, projections[id].name, len);
			*ch = L'*';
			*color = proj_own_color[id];
			break;
	}
}

/* The layer a command's args name, "global" if none; sets *error and
 * returns -1 for an unknown layer, or the character's before there is a
 * character */
static int vis_layer_arg(const struct net_json *v, const char **error)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "layer") : NULL;
	int l;

	*error = NULL;
	if (!name) return LAYER_GLOBAL;
	for (l = 0; l < LAYER_MAX; l++) {
		if (!streq(name, vis_layer_names[l])) continue;
		if (l == LAYER_CHARACTER && !plr_savefile()) {
			*error = "no character";
			return -1;
		}
		return l;
	}
	*error = "no such layer";
	return -1;
}

/**
 * Before a character loads, the game has not read the tile set's files:
 * it does when it leaves start-up.  The Tile Editor on the start menu
 * needs them, so read them now, as for the first race and class, since
 * the files test the character's race and class.  The game reads them
 * again for the character.
 */
static void vis_load_early(void)
{
	const struct player_race *keep_r;
	const struct player_class *keep_c;
	char set[64];

	if (character_generated || net_headless || !player || !races || !classes
			|| !vis_set_name(set, sizeof(set)) || streq(set, vis_set)) {
		return;
	}
	keep_r = player->race;
	keep_c = player->class;
	if (!player->race) player->race = races;
	if (!player->class) player->class = classes;
	reset_visuals(true);
	player->race = keep_r;
	player->class = keep_c;
}

/* Read every layer's files again, dropping the changes made since */
static void vis_reload(void)
{
	vis_reread = true;
	sym_reread = true;
	if (character_generated) {
		reset_visuals(true);
	} else {
		vis_set[0] = '\0';
		vis_load_early();
	}
}

/* Write an entry's tiles as [row, column] for each slot, as the layers
 * up to top give them */
static void vis_jw_tiles(struct net_jw *w, int top, int kind, int id)
{
	int i;

	net_jw_arr(w);
	for (i = 0; i < vis_tiles[kind]; i++) {
		uint8_t a;
		wchar_t c;

		vis_at(top, vis_slot(kind, id, i), &a, &c);
		vis_jw_tile(w, a, c);
	}
	net_jw_arr_end(w);
}

/**
 * VISUALS {"layer": LAYER} lists every entry that can take a tile, by
 * kind, as the layers up to LAYER give it, "global" if none: "id",
 * "name", the letter and color it has without tiles, and "tiles", its
 * tile in each slot as [row, column] on the sheet or null.  Where LAYER
 * changes an entry's tiles, "own" gives the tiles the layers below give,
 * which a reset gives back; otherwise "from" names the lower layer that
 * gives them, if any.  "above" names a higher layer that changes them,
 * which the map shows instead.  "own_symbol" and "symbol_from" say the
 * same of the letter and color.  A flavored object kind says "flavored":
 * the map shows its flavor's tile instead.  "colors" names the colors a
 * symbol can take, by number.  "player_entry" is the player entry for the
 * character's race and class.  "file" is LAYER's file of changes, whether
 * or not it exists yet, and "files" names every layer's.  With
 * {"brief": true} the answer stops after "files", and needs no character.
 */
static const char *net_query_visuals(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	struct net_jw w;
	char set[64], file[1024];
	const char *error;
	bool brief = false;
	int k, id, i, l, layer;

	if (args) net_json_get_bool(args, "brief", &brief);
	layer = vis_layer_arg(v, &error);
	if (layer < 0 && !brief) return error;
	if (layer < 0) layer = LAYER_GLOBAL;
	if (!brief) vis_load_early();
	net_answer_begin(&w, "VISUALS", v);
	if (net_headless || !vis_set_name(set, sizeof(set))
			|| (!brief && !vis_layout())) {
		net_jw_kstr(&w, "set", "");
		net_json_end(&w);
		return NULL;
	}
	net_jw_kstr(&w, "set", set);
	net_jw_kstr(&w, "layer", vis_layer_names[layer]);
	if (vis_file(layer, file, sizeof(file), set)) net_jw_kstr(&w, "file", file);
	net_jw_key(&w, "files");
	net_jw_obj(&w);
	for (l = 0; l < LAYER_MAX; l++) {
		if (vis_file(l, file, sizeof(file), set)) {
			net_jw_kstr(&w, vis_layer_names[l], file);
		}
	}
	net_jw_obj_end(&w);
	if (brief) {
		net_jw_kbool(&w, "brief", true);
		net_json_end(&w);
		return NULL;
	}
	net_jw_key(&w, "lights");
	net_jw_arr(&w);
	for (i = 0; i < LIGHTING_MAX; i++) net_jw_str(&w, vis_lights[i]);
	net_jw_arr_end(&w);
	net_jw_key(&w, "directions");
	net_jw_arr(&w);
	for (i = 0; i < BOLT_MAX; i++) net_jw_str(&w, vis_directions[i]);
	net_jw_arr_end(&w);
	net_jw_key(&w, "colors");
	net_jw_arr(&w);
	for (i = 0; i < BASIC_COLORS; i++) net_jw_str(&w, color_table[i].name);
	net_jw_arr_end(&w);
	/* The character's own race and class, among the player entries */
	if (character_generated && player->race && player->class) {
		net_jw_kint(&w, "player_entry", plr_entry(player->race, player->class));
	}
	net_jw_key(&w, "entries");
	net_jw_arr(&w);
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			char name[120], letter[5];
			wchar_t ch;
			uint8_t sa;
			int color, from = -1, above = -1, sl;
			/* Every race and class has <player>'s letter, monster 0's */
			int sk = k == VIS_PLAYER ? VIS_MONSTER : k;
			int sid = k == VIS_PLAYER ? 0 : id;
			const struct player_race *r;
			const struct player_class *cl;

			/* The player is listed under its own kind, and the
			 * character alone only in their own layer */
			if (!vis_valid(k, id) || (k == VIS_MONSTER && id == 0)) continue;
			if (k == VIS_PLAYER && id == plr_you() && layer != LAYER_CHARACTER) continue;
			vis_describe(k, id, name, sizeof(name), &ch, &color);
			net_jw_obj(&w);
			sl = sym_at(layer, sk, sid, &sa, &ch);
			if (sl == layer) {
				sym_at(layer - 1, sk, sid, &sa, &ch);
				net_utf8(ch, letter);
				net_jw_key(&w, "own_symbol");
				net_jw_obj(&w);
				net_jw_kstr(&w, "char", letter);
				net_jw_kint(&w, "color", sa);
				net_jw_obj_end(&w);
			} else if (sl >= 0) {
				net_jw_kstr(&w, "symbol_from", vis_layer_names[sl]);
			}
			sym_at(layer, sk, sid, &sa, &ch);
			color = sa;
			net_utf8(ch, letter);
			net_jw_kstr(&w, "kind", vis_kind_names[k]);
			net_jw_kint(&w, "id", id);
			net_jw_kstr(&w, "name", name);
			if (k == VIS_PLAYER && plr_race_class(id, &r, &cl)) {
				net_jw_kstr(&w, "group", cl->name);
			} else if (k == VIS_PLAYER && id == plr_you()) {
				/* The character alone, with no tile of their own at first */
				net_jw_kbool(&w, "you", true);
			}
			net_jw_kstr(&w, "char", letter);
			net_jw_kint(&w, "color", color);
			if (k == VIS_OBJECT && k_info[id].flavor
					&& !(k_info[id].tval == TV_SCROLL && k_info[id].aware)) {
				net_jw_kbool(&w, "flavored", true);
			}
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);
				uint8_t a;
				wchar_t c;

				from = MAX(from, vis_at(layer, s, &a, &c));
				for (l = layer + 1; l < LAYER_MAX; l++) {
					if (lay_has[l][s]) above = MAX(above, l);
				}
			}
			net_jw_key(&w, "tiles");
			vis_jw_tiles(&w, layer, k, id);
			if (from == layer) {
				net_jw_key(&w, "own");
				vis_jw_tiles(&w, layer - 1, k, id);
			} else if (from >= 0) {
				net_jw_kstr(&w, "from", vis_layer_names[from]);
			}
			if (above >= 0) net_jw_kstr(&w, "above", vis_layer_names[above]);
			net_jw_obj_end(&w);
		}
	}
	net_jw_arr_end(&w);
	net_json_end(&w);
	return NULL;
}

/* Whether the Tile Editor's commands can work on a layer now: in a game,
 * or on the start menu before a character loads */
static bool sym_ready(int layer, const char **error)
{
	if (layer < 0) return false;
	*error = NULL;
	if (net_headless) *error = "headless mode has no screen";
	else if (!vis_layout()) *error = "the game has not loaded";
	else if (character_generated ? !net_settings_allowed() : net_ctx != CTX_INIT) {
		*error = "busy";
	}
	if (!*error) vis_load_early();
	return *error == NULL;
}

static bool vis_ready(int layer, const char **error)
{
	char set[64];

	if (!sym_ready(layer, error)) return false;
	if (!vis_set_name(set, sizeof(set))) *error = "no tile set";
	return *error == NULL;
}

/**
 * SET_VISUAL {"kind": KIND, "id": N, "tile": [ROW, COLUMN]} gives an
 * entry a tile in a layer, and redraws.  "index" picks one slot, a kind
 * of light or a bolt direction in the order VISUALS lists them; without
 * it every slot takes the tile.  "tile": null gives the entry its letter,
 * and "reset": true takes the layer's change away, so the layers below
 * give the tile.  A tile the layers below already give needs no change.
 */
static const char *net_set_visual_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const struct net_json *tile = args ? net_json_get(args, "tile") : NULL;
	const char *kind_name = args ? net_json_get_str(args, "kind") : NULL;
	const char *error;
	int kind, id, index = -1, row = 0, col = 0, i;
	int layer = vis_layer_arg(v, &error);
	bool reset = false;
	uint8_t letter_a;
	wchar_t letter_c;
	char name[120];
	int color;

	if (!vis_ready(layer, &error)) return error;
	if (!kind_name || !net_json_get_int(args, "id", &id)) {
		return "args must give kind and id";
	}
	for (kind = 0; kind < VIS_MAX; kind++) {
		if (streq(kind_name, vis_kind_names[kind])) break;
	}
	if (!vis_valid(kind, id)) return "no such entry";
	if (kind == VIS_MONSTER && id == 0) return "the player's tiles have kind player";
	if (kind == VIS_PLAYER && id == plr_you() && layer != LAYER_CHARACTER) {
		return "the character's own tile is in the character layer";
	}
	if (net_json_get_int(args, "index", &index)
			&& (index < 0 || index >= vis_tiles[kind])) {
		return "no such index";
	}
	net_json_get_bool(args, "reset", &reset);
	if (!reset) {
		if (!tile) return "args must give tile or reset";
		if (tile->type == NET_JSON_ARRAY) {
			if (!tile->child || !tile->child->next
					|| !net_json_int(tile->child, &row)
					|| !net_json_int(tile->child->next, &col)
					|| row < 0 || row > 126 || col < 0 || col > 127) {
				return "tile must be [row, column]";
			}
		} else if (tile->type != NET_JSON_NULL) {
			return "tile must be [row, column] or null";
		}
	}
	vis_describe(kind, id, name, sizeof(name), &letter_c, &color);
	letter_a = (uint8_t)color;

	for (i = 0; i < vis_tiles[kind]; i++) {
		size_t s = vis_slot(kind, id, i);
		uint8_t na, ba;
		wchar_t nc, bc;

		if (index >= 0 && i != index) continue;
		if (reset) {
			lay_has[layer][s] = false;
			continue;
		}
		if (tile->type == NET_JSON_NULL && kind == VIS_PLAYER && id > 0) {
			/* No tile of its own: <player>'s */
			na = 0;
			nc = 0;
		} else if (tile->type == NET_JSON_NULL) {
			na = letter_a;
			nc = letter_c;
		} else {
			na = (uint8_t)(0x80 | row);
			nc = (wchar_t)(0x80 | col);
		}
		vis_at(layer - 1, s, &ba, &bc);
		lay_has[layer][s] = na != ba || nc != bc;
		lay_a[layer][s] = na;
		lay_c[layer][s] = nc;
	}
	vis_apply_one(kind, id);
	if (kind == VIS_PLAYER) plr_resolve();
	net_redraw();
	return NULL;
}

/* Decode text, which must be one UTF-8 character, into c */
static bool sym_decode(const char *text, wchar_t *c)
{
	const unsigned char *u = (const unsigned char *)text;
	unsigned long v;
	int more, i;

	if (!u || !u[0]) return false;
	if (u[0] < 0x80) {
		v = u[0];
		more = 0;
	} else if ((u[0] & 0xE0) == 0xC0) {
		v = u[0] & 0x1F;
		more = 1;
	} else if ((u[0] & 0xF0) == 0xE0) {
		v = u[0] & 0x0F;
		more = 2;
	} else if ((u[0] & 0xF8) == 0xF0) {
		v = u[0] & 0x07;
		more = 3;
	} else {
		return false;
	}
	for (i = 1; i <= more; i++) {
		if ((u[i] & 0xC0) != 0x80) return false;
		v = (v << 6) | (u[i] & 0x3F);
	}
	if (u[more + 1] || v < 0x20 || v == 0x7F || (v >= 0x80 && v < 0xA0)) {
		return false;
	}
	*c = (wchar_t)v;
	return true;
}

/**
 * SET_SYMBOL {"kind": KIND, "id": N, "char": "x", "color": N} gives an
 * entry, in a layer, the letter and color it has when the map draws
 * letters, one of the colors VISUALS names; the map changes at once if
 * it draws letters.  An effect takes only "color": text mode draws it as
 * * or a line along its path.  "reset": true takes the layer's change
 * away.
 */
static const char *net_set_symbol_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *kind_name = args ? net_json_get_str(args, "kind") : NULL;
	const char *error;
	int kind, id, color;
	int layer = vis_layer_arg(v, &error);
	bool reset = false;
	uint8_t below_a;
	wchar_t below_c, c;
	size_t s;

	if (!sym_ready(layer, &error)) return error;
	if (!kind_name || !net_json_get_int(args, "id", &id)) {
		return "args must give kind and id";
	}
	for (kind = 0; kind < VIS_MAX; kind++) {
		if (streq(kind_name, vis_kind_names[kind])) break;
	}
	if (!vis_valid(kind, id)) return "no such entry";
	/* Every race and class has <player>'s letter, monster 0's */
	if (kind == VIS_PLAYER) {
		kind = VIS_MONSTER;
		id = 0;
	}
	sym_at(layer - 1, kind, id, &below_a, &below_c);
	s = sym_slot(kind, id);
	net_json_get_bool(args, "reset", &reset);
	if (reset) {
		sym_has[layer][s] = false;
	} else {
		if (!net_json_get_int(args, "color", &color)
				|| color < 0 || color >= BASIC_COLORS) {
			return "color must be one VISUALS names";
		}
		c = below_c;
		if (kind != VIS_EFFECT
				&& !sym_decode(net_json_get_str(args, "char"), &c)) {
			return "char must be one letter or sign";
		}
		sym_a[layer][s] = (uint8_t)color;
		sym_c[layer][s] = c;
		sym_has[layer][s] = (uint8_t)color != below_a || c != below_c;
	}
	if (use_graphics == GRAPHICS_NONE) {
		sym_apply_one(kind, id);
		net_redraw();
	}
	return NULL;
}

/* One line of the file of changes, as the pref files write it */
static void vis_write_line(ang_file *f, int kind, int id, int i, uint8_t a,
		wchar_t c)
{
	char name[120];

	switch (kind) {
		case VIS_MONSTER:
			file_putf(f, "monster:%s:0x%02X:0x%02X\n", r_info[id].name, a, (unsigned)c);
			break;
		case VIS_OBJECT:
			object_short_name(name, sizeof(name), k_info[id].name);
			file_putf(f, "object:%s:%s:0x%02X:0x%02X\n",
				tval_find_name(k_info[id].tval), name, a, (unsigned)c);
			break;
		case VIS_FLAVOR:
			file_putf(f, "flavor:%d:0x%02X:0x%02X\n", id, a, (unsigned)c);
			break;
		case VIS_TERRAIN:
			file_putf(f, "feat:%s:%s:0x%02X:0x%02X\n", get_feat_code_name(id),
				vis_lights[i], a, (unsigned)c);
			break;
		case VIS_TRAP:
			file_putf(f, "trap:%s:%s:0x%02X:0x%02X\n", trap_info[id].desc,
				vis_lights[i], a, (unsigned)c);
			break;
		case VIS_PLAYER: {
			const struct player_race *r;
			const struct player_class *cl;

			/* A race and class's own tile, behind the condition the
			 * tile sets use, which vis_on_tiles() reads back */
			if (plr_race_class(id, &r, &cl)) {
				file_putf(f, "?:[AND [EQU $CLASS %s] [EQU $RACE %s] ]\n",
					cl->name, r->name);
			} else if (id == plr_you()) {
				file_putf(f, "?:[EQU $SAVEFILE %s]\n", plr_savefile());
			}
			file_putf(f, "monster:%s:0x%02X:0x%02X\n", r_info[0].name, a, (unsigned)c);
			if (id > 0) file_putf(f, "?:1\n");
			break;
		}
		default:
			file_putf(f, "GF:%s:%s:0x%02X:0x%02X\n", proj_idx_to_name(id),
				vis_directions[i], a, (unsigned)c);
			break;
	}
}

/* Make the folder a file goes in, and the folders above it */
static bool vis_make_parent(const char *path)
{
	char dir[1024];
	char *end;

	my_strcpy(dir, path, sizeof(dir));
	end = strrchr(dir, PATH_SEPC);
	if (!end) return true;
	*end = '\0';
	return dir_exists(dir) || dir_create(dir);
}

/* Open a layer's file at path for writing, the first time, with a note
 * at the top; sets *error when it cannot */
static ang_file *vis_open_file(ang_file *f, const char *path, int layer,
		const char *what, const char **error)
{
	static const char *after[LAYER_MAX] = {
		"its own files", "its own files and its defaults",
		"its own files, its defaults, and your tiles for every character"
	};

	if (f || *error) return f;
	if (!vis_make_parent(path)) {
		*error = "cannot make the folder";
		return NULL;
	}
	f = file_open(path, MODE_WRITE, FTYPE_TEXT);
	if (!f) {
		*error = "cannot write the file";
		return NULL;
	}
	switch (layer) {
		case LAYER_DEFAULTS:
			file_putf(f, "# Fancyband's default %s, from the Tile Editor (Defaults).\n", what);
			break;
		case LAYER_GLOBAL:
			file_putf(f, "# Your %s for every character, from the Tile Editor (Global).\n", what);
			break;
		default:
			file_putf(f, "# %s's %s, from the Tile Editor.\n", player->full_name, what);
			break;
	}
	file_putf(f, "# Fancyband reads this after %s.\n\n", after[layer]);
	return f;
}

/* Write a layer's symbols to its symbols.prf, or remove the file when
 * there are none */
static const char *sym_save(int layer)
{
	char path[1024];
	const char *error = NULL;
	ang_file *f = NULL;
	int k, id, i;

	if (!sym_file(layer, path, sizeof(path))) return "no character";
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			size_t s = sym_slot(k, id);

			if (k == VIS_PLAYER || !vis_valid(k, id) || !sym_has[layer][s]) continue;
			f = vis_open_file(f, path, layer, "symbols", &error);
			if (!f) return error;
			/* Every slot alike; an effect's first gives its color */
			for (i = 0; i < (k == VIS_EFFECT ? 1 : vis_tiles[k]); i++) {
				vis_write_line(f, k, id, i, sym_a[layer][s], sym_c[layer][s]);
			}
		}
	}
	if (f) return file_close(f) ? NULL : "cannot write symbols.prf";
	if (file_exists(path) && !file_delete(path)) return "cannot remove symbols.prf";
	return NULL;
}

/**
 * SAVE_VISUALS writes a layer's symbols to its symbols.prf, and its
 * changes to the tile set in use, if there is one, to its file of
 * changes.  It removes a file that would be empty.  The defaults change
 * only in developer mode, where the engine writes the source tree's.
 */
static const char *net_save_visuals_command(const struct net_json *v)
{
	char path[1024], set[64];
	const char *error;
	ang_file *f = NULL;
	int k, id, i;
	int layer = vis_layer_arg(v, &error);

	if (!sym_ready(layer, &error)) return error;
	if (layer == LAYER_DEFAULTS && !net_defaults_dir[0]) {
		return "the defaults change only in developer mode";
	}
	if ((error = sym_save(layer))) return error;
	if (!vis_set_name(set, sizeof(set))) return NULL;
	if (!vis_file(layer, path, sizeof(path), set)) return "no character";
	for (k = 0; k < VIS_MAX; k++) {
		for (id = 0; id < vis_count[k]; id++) {
			if (!vis_valid(k, id)) continue;
			for (i = 0; i < vis_tiles[k]; i++) {
				size_t s = vis_slot(k, id, i);

				if (!lay_has[layer][s]) continue;
				f = vis_open_file(f, path, layer, format("tiles for %s", set), &error);
				if (!f) return error;
				vis_write_line(f, k, id, i, lay_a[layer][s], lay_c[layer][s]);
			}
		}
	}
	if (f) return file_close(f) ? NULL : "cannot write the file";
	if (file_exists(path) && !file_delete(path)) return "cannot remove the file";
	return NULL;
}

/**
 * RELOAD_VISUALS reads the tile set's files and every layer's saved
 * changes and symbols again, dropping the changes made since, and
 * redraws.
 */
static const char *net_reload_visuals_command(const struct net_json *v)
{
	const char *error;
	int layer = vis_layer_arg(v, &error);

	if (!sym_ready(layer, &error)) return error;
	vis_reload();
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

static bool command_by_name(const char *name, cmd_code *code);

/**
 * RENAME {"name": NAME} changes the character's name, as 'c' on the
 * character sheet does (do_cmd_change_name() in ui-player.c).  The
 * savefile keeps its name.
 */
static const char *net_rename_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *name = args ? net_json_get_str(args, "name") : NULL;

	if (!net_settings_allowed()) return "busy";
	if (!character_generated) return "no character";
	if (arg_force_name) return "You are not allowed to change your name!";
	if (!name || !name[0] || strlen(name) >= 32) return "bad name";
	my_strcpy(player->full_name, name, sizeof(player->full_name));
	player->upkeep->redraw |= (PR_MISC | PR_TITLE);
	handle_stuff(player);
	return NULL;
}

/**
 * DUMP_CHARACTER {"file": NAME} writes a character dump to that file in
 * the user folder, as 'f' on the character sheet does.  The COMMAND
 * result's error is null on success.
 */
static const char *net_dump_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	const char *file = args ? net_json_get_str(args, "file") : NULL;
	char path[1024];
	term *old, t;
	bool ok;

	if (!net_settings_allowed()) return "busy";
	if (!character_generated) return "no character";
	if (!file || !file[0] || strchr(file, '/') || strchr(file, '\\')
			|| strstr(file, "..")) {
		return "bad file name";
	}
	path_build(path, sizeof(path), ANGBAND_DIR_USER, file);
	/* The dump copies the sheet from the screen, so draw it where
	 * nobody sees */
	net_offscreen_begin(&t, &old, 0);
	ok = dump_save(path);
	net_offscreen_end(&t, old);
	return ok ? NULL : "Character dump failed!";
}

/**
 * Whether a command the inventory screen sends may go ahead.  An
 * inscription such as !d on the object, or ^d on anything worn, asks
 * first, as the game's own menu for an object does.
 */
static bool net_then_allowed(const struct net_json *then)
{
	const struct net_json *args = net_json_get(then, "args");
	const char *name = net_json_get_str(then, "cmd");
	int mode = OPT(player, rogue_like_commands) ?
		KEYMAP_MODE_ROGUE : KEYMAP_MODE_ORIG;
	struct object *obj = NULL;
	unsigned char key;
	cmd_code code;

	if (!args || !name || !command_by_name(name, &code)) return true;
	if (net_json_get(args, "item")) {
		obj = net_resolve_item(net_json_get(args, "item"));
	} else if (net_json_get(args, "book")) {
		obj = net_resolve_item(net_json_get(args, "book"));
	}
	if (!obj) return true;
	key = cmd_lookup_key(code, mode);
	return key_confirm_command(key) && get_item_allow(obj, key, code, false);
}

/**
 * Travel from the map screen: its REPLY's "then" may only be a PATHFIND
 * to a grid the player knows a way to, which find_path() checks as the
 * command will.  Returns NULL once the command is pushed, or why not.
 */
static const char *net_map_travel(const struct net_json *then)
{
	const char *name = net_json_get_str(then, "cmd");
	const struct net_json *args = net_json_get(then, "args");
	const struct net_json *point = args ? net_json_get(args, "point") : NULL;
	int x, y;

	if (!name || !streq(name, "PATHFIND")) {
		return "only PATHFIND may follow this screen";
	}
	if (!point || !net_json_get_int(point, "x", &x)
			|| !net_json_get_int(point, "y", &y)
			|| !square_in_bounds_fully(cave, loc(x, y))) {
		return "no such grid";
	}
	if (loc_eq(loc(x, y), player->grid)) return "you are already there";
	if (player->timed[TMD_CONFUSED]) return "you are too confused";
	if (find_path(player, player->grid, loc(x, y), NULL) < 0) {
		return "you know no way there";
	}
	return net_push_command(then);
}

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
	/* The knowledge screen opens the classic equipment comparison, which
	 * it cannot show, and then opens again on that section */
	if (screen == SCREEN_KNOWLEDGE && net_json_get_str(r, "classic")
			&& streq(net_json_get_str(r, "classic"), "equipment")) {
		net_json_free(r);
		equip_cmp_display();
		net_screen(screen, "equipment", classic);
		return;
	}
	then = net_json_get(r, "then");
	if (then && then->type == NET_JSON_OBJECT) {
		const char *error = NULL;
		int id = 0;
		bool have_id = net_json_get_int(then, "id", &id);

		if (screen == SCREEN_INVENTORY && !net_then_allowed(then)) {
			error = "cancelled";
		} else if (screen == SCREEN_MAP) {
			error = net_map_travel(then);
		} else {
			error = net_push_command(then);
		}
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
NET_SCREEN_HOOK(textui_browse_knowledge, SCREEN_KNOWLEDGE, NULL)
NET_SCREEN_HOOK(do_cmd_query_symbol, SCREEN_KNOWLEDGE, "monsters")
NET_SCREEN_HOOK(do_cmd_change_name, SCREEN_CHARACTER, NULL)
NET_SCREEN_HOOK(do_cmd_messages, SCREEN_MESSAGES, NULL)
NET_SCREEN_HOOK(do_cmd_help, SCREEN_HELP, NULL)
NET_SCREEN_HOOK(do_cmd_view_map, SCREEN_MAP, NULL)
NET_SCREEN_HOOK(do_cmd_monlist, SCREEN_LISTS, "monsters")
NET_SCREEN_HOOK(do_cmd_itemlist, SCREEN_LISTS, "items")
NET_SCREEN_HOOK(death_screen, SCREEN_DEATH, NULL)

/**
 * Look and target mode, for target_set_interactive_hook, so they go to
 * the client however they start: by a key, the aiming prompt's '*', or
 * the mouse menu's Look.  The request's "mode" is "look" or "target", as
 * LOOK takes it; "grid" is a grid the player picked to start on, and
 * "target" the target the player had.  As the text UI's own loop does,
 * the target is cancelled first; while the screen is open the client
 * sets one with SET_TARGET.  The REPLY may carry a PATHFIND as "then",
 * when "pathfind" says the player may travel from here.  Returns false
 * to let the text UI's own loop run.
 */
static bool net_target_interactive(int mode, int x, int y,
		bool allow_pathfinding)
{
	enum net_screen screen = (mode & TARGET_KILL) ? SCREEN_TARGET : SCREEN_LOOK;
	struct net_jw w;
	struct net_json *r;
	const struct net_json *then;
	bool use_classic = false, had = target_okay();
	struct loc old;
	struct monster *old_mon = target_get_monster();

	if (!screen_claimed[screen] || net_borg_playing() || net_is_exiting()
			|| net_keys_queued() || !character_dungeon) {
		return false;
	}
	target_get(&old);
	target_set_monster(NULL);

	net_request_begin_named(&w, "screen", NULL);
	net_jw_kstr(&w, "name", screen_names[screen]);
	net_jw_kstr(&w, "mode", (mode & TARGET_KILL) ? "target" : "look");
	if (x >= 0 && y >= 0 && square_in_bounds_fully(cave, loc(x, y))) {
		net_jw_key(&w, "grid");
		net_jw_obj(&w);
		net_jw_kint(&w, "x", x);
		net_jw_kint(&w, "y", y);
		net_jw_obj_end(&w);
	}
	if (had) {
		net_jw_key(&w, "target");
		net_jw_obj(&w);
		net_jw_kint(&w, "x", old.x);
		net_jw_kint(&w, "y", old.y);
		if (old_mon) net_jw_kint(&w, "monster", old_mon->midx);
		net_jw_obj_end(&w);
	}
	net_jw_kbool(&w, "pathfind", allow_pathfinding);
	screen_open = true;
	r = net_request_wait(&w);
	screen_open = false;

	if (r && net_json_get_bool(r, "classic", &use_classic) && use_classic) {
		net_json_free(r);
		return false;
	}
	then = r ? net_json_get(r, "then") : NULL;
	if (then && then->type == NET_JSON_OBJECT) {
		const char *name = net_json_get_str(then, "cmd");
		const char *error = NULL;
		int id = 0;
		bool have_id = net_json_get_int(then, "id", &id);

		if (!allow_pathfinding || !name || !streq(name, "PATHFIND")) {
			error = "only PATHFIND may follow this screen";
		} else {
			error = net_push_command(then);
		}
		net_command_result(id, have_id, error == NULL, error);
	}
	net_json_free(r);

	/* As the text UI's loop ends */
	verify_panel();
	handle_stuff(player);
	return true;
}

/**
 * The command menu, for action_menu_hook, so Enter's menu goes to the
 * client.  The REPLY names the command chosen by "list" and "command",
 * as COMMANDS numbers them; the text UI then runs it as it runs one
 * chosen from its own menu.  A REPLY without one chooses nothing.
 * Returns false to let the text UI's own menu run.
 */
static bool net_action_menu(struct cmd_info **chosen)
{
	struct net_jw w;
	struct net_json *r;
	bool use_classic = false;
	int li = -1, ci = -1, n = 0;

	*chosen = NULL;
	if (!screen_claimed[SCREEN_COMMANDS] || net_borg_playing()
			|| net_is_exiting() || net_keys_queued()) {
		return false;
	}
	net_request_begin_named(&w, "screen", NULL);
	net_jw_kstr(&w, "name", screen_names[SCREEN_COMMANDS]);
	screen_open = true;
	r = net_request_wait(&w);
	screen_open = false;
	if (!r) return true;
	if (net_json_get_bool(r, "classic", &use_classic) && use_classic) {
		net_json_free(r);
		return false;
	}
	while (cmds_all[n].name) n++;
	if (net_json_get_int(r, "list", &li) && net_json_get_int(r, "command", &ci)
			&& li >= 0 && li < n && ci >= 0
			&& (size_t)ci < cmds_all[li].len) {
		struct cmd_info *c = &cmds_all[li].list[ci];

		if (c->cmd || c->hook) *chosen = c;
	}
	net_json_free(r);
	return true;
}

/**
 * ------------------------------------------------------------------------
 * Screens that take commands
 *
 * The store and birth screens stay open while the player shops or makes
 * a character.  While one is open, the client sends that context's game
 * commands, such as BUY or CHOOSE_RACE, as COMMANDs.  The engine runs
 * each at once and sends its result once it has run, so the client knows
 * the game has changed.  The screen's REPLY closes it.
 * ------------------------------------------------------------------------ */

/**
 * After a reset, a new character has the last race and class in the
 * game's lists.  The birth menus start on the first ones, which
 * do_cmd_birth_init() gave it, so give it those again.  A quick start
 * keeps the last character's.
 */
static void net_birth_reset_choices(void)
{
	if (!birth_quickstart) {
		player_generate(player, player_id2race(0), player_id2class(0), false);
	}
}

/**
 * Run the command the open screen sent, as the store menu or the birth
 * menus run theirs, and send its result.
 */
static void net_session_run(enum net_screen screen)
{
	if (screen == SCREEN_STORE) {
		/* Messages go to the client as events; none may wait for a key
		 * behind the store screen */
		bool more = player->opts.opt[OPT_auto_more];

		player->opts.opt[OPT_auto_more] = true;
		cmdq_pop(CTX_STORE);
		event_signal(EVENT_INVENTORY);
		event_signal(EVENT_EQUIPMENT);
		notice_stuff(player);
		handle_stuff(player);
		player->opts.opt[OPT_auto_more] = more;
		msg_flag = false;
	} else {
		cmdq_execute(CTX_BIRTH);
		switch (session_code) {
			case CMD_ROLL_STATS:
			case CMD_PREV_STATS:
				birth_rolled = true;
				break;
			case CMD_BIRTH_RESET:
				net_birth_reset_choices();
				birth_rolled = false;
				break;
			case CMD_CHOOSE_RACE:
			case CMD_CHOOSE_CLASS:
			case CMD_RESET_STATS:
				birth_rolled = false;
				break;
			default:
				break;
		}
	}
	command_ready = false;
	net_command_result(session_cmd_id, session_have_id, true, NULL);
}

/**
 * Hand the store or birth screen to the client and run the commands it
 * sends until it closes.  Returns false if the client wants the classic
 * screen.  The birth screen stays open until the character is made.
 */
static bool net_session(enum net_screen screen)
{
	bool use_classic = false;

	if (!screen_claimed[screen] || net_borg_playing() || net_is_exiting()) {
		return false;
	}

	screen_open = true;
	command_ready = false;
	while (!net_is_exiting()) {
		struct net_jw w;
		struct net_json *r;

		net_request_begin_named(&w, "screen", NULL);
		net_jw_kstr(&w, "name", screen_names[screen]);
		net_json_end(&w);
		net_flush();
		session_id = request_id;
		while (!net_is_exiting()) {
			reply_wanted = session_id;
			while (!reply && !command_ready && !net_is_exiting()) {
				net_wait_input();
			}
			reply_wanted = 0;
			if (reply) break;
			if (command_ready) net_session_run(screen);
			if (screen == SCREEN_BIRTH && character_generated) break;
		}
		session_id = 0;
		r = reply;
		reply = NULL;
		if (r) net_json_get_bool(r, "classic", &use_classic);
		net_json_free(r);

		/* Only a character, or the classic menus, end character
		 * creation; ask again */
		if (screen != SCREEN_BIRTH || character_generated || use_classic) {
			break;
		}
	}
	screen_open = false;
	return !use_classic;
}

static bool net_store_screen(void)
{
	return net_session(SCREEN_STORE);
}

static bool net_birth_screen(void)
{
	if (!screen_claimed[SCREEN_BIRTH] || net_borg_playing()) return false;

	/* As the text UI does, start from a reset character */
	cmdq_push(CMD_BIRTH_RESET);
	cmdq_execute(CTX_BIRTH);
	net_birth_reset_choices();
	birth_rolled = false;
	return net_session(SCREEN_BIRTH);
}

static const struct {
	enum net_screen screen;
	void (*classic)(void);
	void (*hook)(void);
} screen_hooks[] = {
	{ SCREEN_OPTIONS, do_cmd_xxx_options, net_screen_do_cmd_xxx_options },
	{ SCREEN_INVENTORY, do_cmd_inven, net_screen_do_cmd_inven },
	{ SCREEN_INVENTORY, do_cmd_equip, net_screen_do_cmd_equip },
	{ SCREEN_INVENTORY, do_cmd_quiver, net_screen_do_cmd_quiver },
	{ SCREEN_KNOWLEDGE, textui_browse_knowledge,
		net_screen_textui_browse_knowledge },
	{ SCREEN_KNOWLEDGE, do_cmd_query_symbol,
		net_screen_do_cmd_query_symbol },
	{ SCREEN_CHARACTER, do_cmd_change_name, net_screen_do_cmd_change_name },
	{ SCREEN_MESSAGES, do_cmd_messages, net_screen_do_cmd_messages },
	{ SCREEN_HELP, do_cmd_help, net_screen_do_cmd_help },
	{ SCREEN_MAP, do_cmd_view_map, net_screen_do_cmd_view_map },
	{ SCREEN_LISTS, do_cmd_monlist, net_screen_do_cmd_monlist },
	{ SCREEN_LISTS, do_cmd_itemlist, net_screen_do_cmd_itemlist },
};

/**
 * Point the text UI's commands for the claimed screens at the hand-over
 * functions, or back at their own.  The death, store, and birth screens
 * have hooks of their own, since no command opens them.
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
	if (screen_claimed[SCREEN_LOOK] || screen_claimed[SCREEN_TARGET]) {
		target_set_interactive_hook = claim ? net_target_interactive : NULL;
	}
	if (screen_claimed[SCREEN_DEATH]) {
		death_screen_hook = claim ? net_screen_death_screen : NULL;
	}
	if (screen_claimed[SCREEN_COMMANDS]) {
		action_menu_hook = claim ? net_action_menu : NULL;
	}
	if (screen_claimed[SCREEN_STORE]) {
		store_screen_hook = claim ? net_store_screen : NULL;
	}
	if (screen_claimed[SCREEN_BIRTH]) {
		birth_screen_hook = claim ? net_birth_screen : NULL;
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

static const char *net_cmd_name(cmd_code code)
{
	return (size_t)code < N_ELEMENTS(cmd_names) ? cmd_names[code] : "NULL";
}

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
 * SET_TARGET {"monster": MIDX} targets a monster, by the index LOOK gives
 * it, if the game lets it be targeted; {"x": X, "y": Y} targets a grid;
 * {"clear": true} drops the target.  It works at the command prompt and
 * while a screen or a prompt is open, such as the aiming prompt, and it
 * tracks the monster's health and recall, as look mode does.
 */
static const char *net_set_target_command(const struct net_json *v)
{
	const struct net_json *args = net_json_get(v, "args");
	bool clear = false;
	int midx, x, y;

	if (!args) return "args must give monster, or x and y, or clear";
	if (!character_dungeon || !cave || player->is_dead) return "no level";
	if (!net_settings_allowed() && !reply_wanted) return "busy";

	if (net_json_get_int(args, "monster", &midx)) {
		struct monster *mon = (midx > 0 && midx < cave_monster_max(cave)) ?
			cave_monster(cave, midx) : NULL;

		if (!mon || !mon->race || !target_able(mon)) {
			return "that cannot be targeted";
		}
		target_set_monster(mon);
		monster_race_track(player->upkeep, mon->race);
		health_track(player->upkeep, mon);
	} else if (net_json_get_int(args, "x", &x)
			&& net_json_get_int(args, "y", &y)) {
		if (!square_in_bounds_fully(cave, loc(x, y))) return "off the level";
		target_set_location(y, x);
	} else if (net_json_get_bool(args, "clear", &clear) && clear) {
		target_set_monster(NULL);
	} else {
		return "args must give monster, or x and y, or clear";
	}
	if (!net_headless) handle_stuff(player);
	return NULL;
}

/**
 * Push the event that wakes the text UI's wait for a command.  The text
 * UI flushes its input before it waits, and reads what the client sent
 * meanwhile as it does, so the flush can drop this event; wake_pending
 * has net_game_key_wait() push it again.
 */
static void net_wake_text_ui(void)
{
	ui_event e = EVENT_EMPTY;

	e.type = EVT_RESIZE;
	Term_event_push(&e);
	wake_pending = true;
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
	if (streq(name, "MAP")) return net_query_map(v);
	if (streq(name, "VISUALS")) return net_query_visuals(v);
	if (streq(name, "SAVE")) return net_save_command();
	if (streq(name, "SET_OPTION")) return net_set_option_command(v);
	if (streq(name, "SAVE_OPTION_DEFAULTS")) return net_save_defaults_command(v);
	if (streq(name, "GRAPHICS")) return net_graphics_command(v);
	if (streq(name, "PANELS")) return net_panels_command(v);
	if (streq(name, "BORG")) return net_borg_command(v);
	if (streq(name, "SET_VISUAL")) return net_set_visual_command(v);
	if (streq(name, "SET_SYMBOL")) return net_set_symbol_command(v);
	if (streq(name, "SET_TARGET")) return net_set_target_command(v);
	if (streq(name, "SAVE_VISUALS")) return net_save_visuals_command(v);
	if (streq(name, "RENAME")) return net_rename_command(v);
	if (streq(name, "DUMP_CHARACTER")) return net_dump_command(v);
	if (streq(name, "RELOAD_VISUALS")) return net_reload_visuals_command(v);
	if (streq(name, "LEAVE_STORE")) {
		if (!net_headless || net_ctx != CTX_STORE || !in_get_cmd) {
			return "not in a store";
		}
		leaving_store = true;
		command_ready = true;
		return NULL;
	}

	if (!command_by_name(name, &code)) return "unknown command";
	ctx = command_context(code);
	if (ctx == CTX_INIT) return "command not allowed";
	if (ctx != net_ctx) return "command not allowed here";
	if (session_id) {
		/* The open store or birth screen's command runs at once, and
		 * its result goes out once it has run */
		if (command_ready || reply_wanted != session_id) return "busy";
		error = net_push_command(v);
		if (error) return error;
		command_ready = true;
		session_code = code;
		session_have_id = net_json_get_int(v, "id", &session_cmd_id);
		result_deferred = true;
		return NULL;
	}
	/* A prompt waiting for its reply may be inside the wait, and its
	 * command could flush this one from the queue */
	if (!in_get_cmd || command_ready || screen_open || reply_wanted) {
		return "busy";
	}
	if (!net_headless && (net_ctx != CTX_GAME || screen_save_depth > 0)) {
		/* The text UI has a menu open; only its top-level wait is safe */
		return "busy";
	}

	error = net_push_command(v);
	if (error) return error;
	command_ready = true;

	/* Wake the text UI's wait; it redraws on a resize event and returns,
	 * and then the game runs the queued command */
	if (!net_headless) net_wake_text_ui();
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
	if (result_deferred) {
		result_deferred = false;
	} else {
		net_command_result(id, have_id, error == NULL, error);
	}
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
 * client with "waits" hears about once.  Returns true if it pushed an
 * event for the text UI to read rather than wait.
 */
bool net_game_key_wait(void)
{
	if (cap_waits && !net_headless && net_ctx == CTX_INIT && !splash_waited) {
		splash_waited = true;
		net_send_wait("splash");
	}

	/* The flush before the wait dropped the event for a command */
	if (wake_pending && in_get_cmd && command_ready) {
		net_wake_text_ui();
		wake_pending = false;
		return true;
	}
	return false;
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
	wake_pending = false;
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
	visuals_loaded_hook = net_on_visuals;
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
	leaving_store = false;
	while (!leaving_store && !net_is_exiting() && !player->is_dead) {
		net_get_cmd(CTX_STORE);
		if (!leaving_store) cmdq_pop(CTX_STORE);
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
