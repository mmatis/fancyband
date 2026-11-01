/**
 * \file net-glossary.c
 * \brief The net front end's GLOSSARY: what each term the game's screens
 * name means
 *
 * Every race, class, stat, skill, ability, element, curse, and status
 * effect, with its text and the names a screen may show it by.  The text
 * is the data files' one-line description.  Races, classes, stats, and
 * skills have none, so theirs is the manual's, from
 * lib/help/descriptions.txt, which scripts/extract-descriptions writes from
 * the manual's pages in docs.
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
 */

#include "angband.h"
#include "init.h"
#include "net-glossary.h"
#include "net-json.h"
#include "obj-curse.h"
#include "obj-properties.h"
#include "object.h"
#include "player.h"
#include "player-timed.h"
#include "project.h"
#include "ui-entry.h"
#include "z-textblock.h"

/* player-timed.c */
extern const char *list_player_flag_names[];

/**
 * A name a screen may show a term by.  A label-only name, such as "Fear"
 * or "Save", is a common word, which the client marks only where a screen
 * shows it as a label, and not in running text.
 */
struct gl_name {
	char *text;
	bool label;
};

struct gl_term {
	char *id;
	const char *kind;
	char *title;
	textblock *tb;
	bool manual;
	struct gl_name *names;
	int n_names, a_names;
};

struct gl_item {
	char *name;
	const char *id;
};

/**
 * A series is a lead phrase an item description puts before a list, as
 * "Provides resistance to fire, cold, poison.", with the term each item
 * of the list names.
 */
struct gl_series {
	const char *lead;
	struct gl_item items[ELEM_MAX + OF_MAX];
	int n;
};

struct gl_entry {
	char *ids;
	char *text;
};

static struct gl_term *terms;
static int n_terms, a_terms;
static struct gl_entry *manual;
static int n_manual, a_manual;

/**
 * ------------------------------------------------------------------------
 * The manual's descriptions
 * ------------------------------------------------------------------------ */

static void manual_read(void)
{
	char path[1024];
	char *buf = mem_alloc(8192);
	ang_file *f;
	struct gl_entry *cur = NULL;

	path_build(path, sizeof(path), ANGBAND_DIR_HELP, "descriptions.txt");
	f = file_open(path, MODE_READ, FTYPE_TEXT);
	if (!f) {
		mem_free(buf);
		return;
	}
	while (file_getl(f, buf, 8192)) {
		if (buf[0] == '#' || buf[0] == '\0') continue;
		if (prefix(buf, "@ ")) {
			if (n_manual == a_manual) {
				a_manual = a_manual ? 2 * a_manual : 64;
				manual = mem_realloc(manual, a_manual * sizeof(*manual));
			}
			cur = &manual[n_manual++];
			/* Spaces round the ids, so " id " finds a whole one */
			cur->ids = string_make(format(" %s ", buf + 2));
			cur->text = NULL;
			continue;
		}
		if (!cur) continue;
		if (cur->text) {
			char *both = string_make(format("%s\n\n%s", cur->text, buf));

			string_free(cur->text);
			cur->text = both;
		} else {
			cur->text = string_make(buf);
		}
	}
	file_close(f);
	mem_free(buf);
}

static const char *manual_text(const char *id)
{
	char key[128];
	int i;

	strnfmt(key, sizeof(key), " %s ", id);
	for (i = 0; i < n_manual; i++) {
		if (strstr(manual[i].ids, key)) return manual[i].text;
	}
	return NULL;
}

/**
 * ------------------------------------------------------------------------
 * Terms
 * ------------------------------------------------------------------------ */

static struct gl_term *term_find(const char *id)
{
	int i;

	for (i = 0; i < n_terms; i++) {
		if (streq(terms[i].id, id)) return &terms[i];
	}
	return NULL;
}

/**
 * Add a term, with line, the data files' one line about it, or the
 * manual's text when line is NULL or empty, as for races, classes, stats,
 * and skills, which the data files do not describe.  A term with no text
 * at all is left out, and NULL returned.
 */
static struct gl_term *term_add(const char *kind, const char *id,
		const char *title, const char *line)
{
	const char *text = (line && line[0]) ? NULL : manual_text(id);
	struct gl_term *t;

	if (term_find(id)) return NULL;
	if (!text && (!line || !line[0])) return NULL;
	if (n_terms == a_terms) {
		a_terms = a_terms ? 2 * a_terms : 256;
		terms = mem_realloc(terms, a_terms * sizeof(*terms));
	}
	t = &terms[n_terms++];
	memset(t, 0, sizeof(*t));
	t->id = string_make(id);
	t->kind = kind;
	t->title = string_make(title);
	t->tb = textblock_new();
	t->manual = text != NULL;
	textblock_append(t->tb, "%s", text ? text : line);
	return t;
}

static void name_add(struct gl_term *t, const char *text, bool label)
{
	int i;

	if (!t || !text || !text[0]) return;
	for (i = 0; i < t->n_names; i++) {
		if (streq(t->names[i].text, text)) {
			/* Running text wins over label only */
			if (!label) t->names[i].label = false;
			return;
		}
	}
	if (t->n_names == t->a_names) {
		t->a_names = t->a_names ? 2 * t->a_names : 8;
		t->names = mem_realloc(t->names, t->a_names * sizeof(*t->names));
	}
	t->names[t->n_names].text = string_make(text);
	t->names[t->n_names].label = label;
	t->n_names++;
}

/* Add text, its lowercase form, and its form with a capital */
static void names_add(struct gl_term *t, const char *text, bool label)
{
	char buf[256];
	size_t i;

	if (!text) return;
	name_add(t, text, label);
	my_strcpy(buf, text, sizeof(buf));
	for (i = 0; buf[i]; i++) buf[i] = tolower((unsigned char)buf[i]);
	name_add(t, buf, label);
	my_strcap(buf);
	name_add(t, buf, label);
}

static void capital(char *buf, size_t len, const char *s)
{
	my_strcpy(buf, s, len);
	my_strcap(buf);
}

/**
 * ------------------------------------------------------------------------
 * Each kind of term
 * ------------------------------------------------------------------------ */

static void add_races_and_classes(void)
{
	struct player_race *r;
	struct player_class *c;

	for (r = races; r; r = r->next) {
		name_add(term_add("race", format("race:%s", r->name), r->name, NULL),
			r->name, false);
	}
	for (c = classes; c; c = c->next) {
		name_add(term_add("class", format("class:%s", c->name), c->name,
			NULL), c->name, false);
	}
}

static const char *stat_codes[STAT_MAX] = { "STR", "INT", "WIS", "DEX", "CON" };

static void add_stats(void)
{
	int i;

	for (i = 0; i < STAT_MAX; i++) {
		struct obj_property *prop = lookup_obj_property(OBJ_PROPERTY_STAT, i);
		char title[64], small[8];
		struct gl_term *t;

		capital(title, sizeof(title), prop ? prop->name : stat_codes[i]);
		t = term_add("stat", format("stat:%s", stat_codes[i]), title, NULL);
		names_add(t, title, false);
		name_add(t, stat_codes[i], false);
		capital(small, sizeof(small), stat_codes[i]);
		for (size_t k = 1; small[k]; k++) {
			small[k] = tolower((unsigned char)small[k]);
		}
		name_add(t, small, true);
	}
}

/**
 * The skills, as birth.rst describes them, with the names the birth
 * menus, the character sheet, and the manual's tables give them.
 */
static const struct {
	const char *code, *title;
	const char *running[4], *labels[5];
} skills[] = {
	{ "INFRA", "Infravision", { "Infravision", "infravision" }, { "Infra" } },
	{ "TO_HIT_MELEE", "Fighting", { "fighting skill" },
		{ "Fighting", "Fight", "Hit", "Melee" } },
	{ "TO_HIT_BOW", "Shooting", { "shooting skill" },
		{ "Shooting", "Shoot", "Bows" } },
	{ "TO_HIT_THROW", "Throwing", { "throwing skill" },
		{ "Throwing", "Throw" } },
	{ "SAVE", "Saving Throw", { "Saving Throw", "Saving Throws",
		"saving throw", "saving throws" }, { "Save" } },
	{ "STEALTH", "Stealth", { "Stealth", "stealth" }, { NULL } },
	{ "DISARM_PHYS", "Disarming physical traps", { "disarming" },
		{ "Disarm - phys.", "Disarm", "DisarmP", "Disarming" } },
	{ "DISARM_MAGIC", "Disarming magical traps", { NULL },
		{ "Disarm - magic", "DisarmM" } },
	{ "DEVICE", "Magic Devices", { "Magic Devices", "Magical Devices",
		"magic devices", "magical devices" }, { "Devices", "Device" } },
	{ "SEARCH", "Searching", { "Perception", "perception",
		"searching skill" }, { "Searching", "Search" } },
};

static void add_skills(void)
{
	size_t i, k;

	for (i = 0; i < N_ELEMENTS(skills); i++) {
		struct gl_term *t = term_add("skill",
			format("skill:%s", skills[i].code), skills[i].title, NULL);

		for (k = 0; k < N_ELEMENTS(skills[i].running); k++) {
			name_add(t, skills[i].running[k], false);
		}
		for (k = 0; k < N_ELEMENTS(skills[i].labels); k++) {
			name_add(t, skills[i].labels[k], true);
		}
	}
}

/* An ability's id, from its type and index */
static void ability_id(char *buf, size_t len, const struct player_ability *a)
{
	if (streq(a->type, "player")) {
		strnfmt(buf, len, "ability:%s", list_player_flag_names[a->index]);
	} else if (streq(a->type, "object")) {
		strnfmt(buf, len, "ability:%s", list_obj_flag_names[a->index]);
	} else {
		const char *kind = a->value == 3 ? "IM" : a->value < 0 ? "VULN" : "RES";

		strnfmt(buf, len, "ability:%s_%s", kind,
			list_element_names[a->index]);
	}
}

static struct gl_series series[] = {
	{ "Provides resistance to", { { NULL, NULL } }, 0 },
	{ "Provides immunity to", { { NULL, NULL } }, 0 },
	{ "Makes you vulnerable to", { { NULL, NULL } }, 0 },
	{ "Provides protection from", { { NULL, NULL } }, 0 },
	{ "Sustains", { { NULL, NULL } }, 0 },
	{ "Cannot be harmed by", { { NULL, NULL } }, 0 },
	{ "Can be destroyed by", { { NULL, NULL } }, 0 },
};

static void series_add(int s, const char *name, const char *id)
{
	struct gl_series *g = &series[s];
	int i;

	if (!id || !name || g->n >= (int)N_ELEMENTS(g->items)) return;
	for (i = 0; i < g->n; i++) {
		if (streq(g->items[i].name, name)) return;
	}
	g->items[g->n].name = string_make(name);
	g->items[g->n].id = id;
	g->n++;
}

static void add_abilities(void)
{
	struct player_ability *a;

	for (a = player_abilities; a; a = a->next) {
		char id[96];
		struct gl_term *t;

		ability_id(id, sizeof(id), a);
		t = term_add("ability", id, a->name, a->desc);
		if (!t) continue;
		names_add(t, a->name, false);
		if (streq(a->type, "object")) {
			struct obj_property *prop =
				lookup_obj_property(OBJ_PROPERTY_FLAG, a->index);

			if (prop) {
				names_add(t, prop->name, false);
				if (prop->subtype == OFT_PROT) {
					series_add(3, prop->desc, t->id);
				} else if (prop->desc) {
					/* Item descriptions print it as a sentence */
					name_add(t, prop->desc, false);
				}
			}
			if (a->index >= OF_SUST_STR && a->index <= OF_SUST_CON) {
				struct obj_property *stat = lookup_obj_property(
					OBJ_PROPERTY_STAT, a->index - OF_SUST_STR);

				if (stat) series_add(4, stat->name, t->id);
			}
		} else if (streq(a->type, "element")) {
			const char *el = projections[a->index].name;
			int s = a->value == 3 ? 1 : a->value < 0 ? 2 : 0;
			const char *how = a->value == 3 ? "immunity to" :
				a->value < 0 ? "vulnerability to" : "resistance to";

			series_add(s, el, t->id);
			name_add(t, format("%s %s", how, el), false);
		}
	}
}

/* Elements too common a word to mark in running text */
static bool element_common(int i)
{
	switch (i) {
		case ELEM_NEXUS: case ELEM_NETHER: case ELEM_CHAOS:
		case ELEM_DISEN: case ELEM_SHARD: case ELEM_INERTIA:
		case ELEM_GRAVITY: case ELEM_PLASMA:
			return false;
		default:
			return true;
	}
}

static void add_elements(void)
{
	int i;

	for (i = 0; i < ELEM_MAX; i++) {
		char id[64], title[64], text[512] = "";
		const struct player_ability *a;
		struct gl_term *t;

		/* What resisting, being immune to, and being vulnerable to it
		 * do, in the abilities' own lines */
		for (a = player_abilities; a; a = a->next) {
			if (!streq(a->type, "element") || a->index != i) continue;
			my_strcat(text, format("%s%s: %s", text[0] ? "\n" : "",
				a->name, a->desc), sizeof(text));
		}
		strnfmt(id, sizeof(id), "element:%s", list_element_names[i]);
		capital(title, sizeof(title), projections[i].name);
		t = term_add("element", id, title, text);
		if (!t) continue;
		names_add(t, projections[i].name, element_common(i));
		series_add(5, projections[i].name, t->id);
		series_add(6, projections[i].name, t->id);
	}
}

static void add_curses(void)
{
	int i;

	for (i = 1; i < z_info->curse_max; i++) {
		const struct curse *c = &curses[i];
		char id[96], title[96], text[256];
		struct gl_term *t;
		size_t k;

		if (!c->name) continue;
		my_strcpy(id, format("curse:%s", c->name), sizeof(id));
		for (k = 0; id[k]; k++) {
			if (id[k] == ' ') id[k] = '_';
		}
		capital(title, sizeof(title), format("%s curse", c->name));
		strnfmt(text, sizeof(text), "An item with this curse %s.",
			c->desc ? c->desc : "does harm");
		t = term_add("curse", id, title, c->desc ? text : NULL);
		names_add(t, format("%s curse", c->name), false);
		names_add(t, c->name, true);
		/* Item descriptions say "It <desc>." */
		name_add(t, c->desc, false);
	}
}

/* What makes a status effect fail, as a name */
static const char *timed_fail_name(const struct timed_failure *f)
{
	struct player_ability *a;

	switch (f->code) {
		case TMD_FAIL_FLAG_OBJECT:
		case TMD_FAIL_FLAG_PLAYER:
			for (a = player_abilities; a; a = a->next) {
				bool object = streq(a->type, "object");

				if ((f->code == TMD_FAIL_FLAG_OBJECT) == object
						&& !streq(a->type, "element") && a->index == f->idx) {
					return a->name;
				}
			}
			return NULL;
		case TMD_FAIL_FLAG_RESIST:
			for (a = player_abilities; a; a = a->next) {
				if (streq(a->type, "element") && a->value == 1
						&& a->index == f->idx) {
					return a->name;
				}
			}
			return NULL;
		case TMD_FAIL_FLAG_TIMED_EFFECT:
			return (f->idx >= 0 && f->idx < TMD_MAX) ?
				timed_effects[f->idx].desc : NULL;
		default:
			return NULL;
	}
}

static void add_timed(void)
{
	int i;

	for (i = 0; i < TMD_MAX; i++) {
		const struct timed_effect_data *e = &timed_effects[i];
		const struct timed_grade *g;
		const struct timed_failure *f;
		char title[64] = "", text[1024] = "", grades[512] = "";
		int n_grades = 0;
		struct gl_term *t;

		if (!e->name || !e->desc) continue;
		for (g = e->grade; g; g = g->next) {
			if (!g->name || strlen(g->name) < 2) continue;
			if (!title[0]) {
				my_strcpy(title, g->name, sizeof(title));
				if (g->up_msg && strlen(g->up_msg) > 1) {
					my_strcat(text, g->up_msg, sizeof(text));
				}
			}
			my_strcat(grades, n_grades ? ", " : "", sizeof(grades));
			my_strcat(grades, g->name, sizeof(grades));
			n_grades++;
		}
		if (!title[0] || n_grades > 1) capital(title, sizeof(title), e->desc);
		else if (title[strlen(title) - 1] == '!') title[strlen(title) - 1] = 0;
		if (!text[0]) {
			capital(text, sizeof(text), format("%s.", e->desc));
		}
		if (n_grades > 1) {
			my_strcat(text, format("  It goes by degrees: %s.", grades),
				sizeof(text));
		}
		for (f = e->fail; f; f = f->next) {
			const char *by = timed_fail_name(f);

			if (by && f->code != TMD_FAIL_FLAG_TIMED_EFFECT) {
				my_strcat(text, format("  %s prevents it.", by), sizeof(text));
			}
		}
		t = term_add("timed", format("timed:%s", e->name), title, text);
		for (g = e->grade; g; g = g->next) {
			char name[64];
			size_t n;

			if (!g->name || strlen(g->name) < 2) continue;
			my_strcpy(name, g->name, sizeof(name));
			n = strlen(name);
			while (n > 0 && (name[n - 1] == '!' || name[n - 1] == '.')) {
				name[--n] = '\0';
			}
			name_add(t, name, true);
		}
		names_add(t, e->desc, true);
	}
}

/**
 * The labels the game's screens show, as the equipment comparison and the
 * character sheet's abilities table do, from ui_entry.txt
 */
static void label_visit(void *closure, const char *label,
		const struct player_ability *a, int type, int index)
{
	char id[96] = "";
	struct gl_term *t;
	size_t n = strlen(label);

	(void)closure;
	/* Short labels such as "rF" say too little to match on */
	while (n > 0 && label[n - 1] == ' ') n--;
	if (n < 3) return;
	if (a) {
		/* A class's ability bound to a general row, as Extra Shots to
		 * "Shots", would describe the row wrongly */
		if (streq(a->type, "player")) return;
		if (streq(a->type, "element")) {
			strnfmt(id, sizeof(id), "element:%s", list_element_names[a->index]);
		} else {
			ability_id(id, sizeof(id), a);
		}
	} else if (type == OBJ_PROPERTY_STAT && index >= 0 && index < STAT_MAX) {
		strnfmt(id, sizeof(id), "stat:%s", stat_codes[index]);
	} else if (type == OBJ_PROPERTY_MOD) {
		if (index == OBJ_MOD_STEALTH) my_strcpy(id, "skill:STEALTH", sizeof(id));
		else if (index == OBJ_MOD_SEARCH) my_strcpy(id, "skill:SEARCH", sizeof(id));
		else if (index == OBJ_MOD_INFRA) my_strcpy(id, "skill:INFRA", sizeof(id));
	} else if (type == OBJ_PROPERTY_FLAG) {
		strnfmt(id, sizeof(id), "ability:%s", list_obj_flag_names[index]);
	}
	if (!id[0]) return;
	t = term_find(id);
	if (t) {
		char buf[128];

		my_strcpy(buf, label, MIN(sizeof(buf), n + 1));
		name_add(t, buf, true);
	}
}

/**
 * ------------------------------------------------------------------------
 * The answer
 * ------------------------------------------------------------------------ */

static void glossary_free(void)
{
	int i, k;
	size_t s;

	for (i = 0; i < n_terms; i++) {
		string_free(terms[i].id);
		string_free(terms[i].title);
		textblock_free(terms[i].tb);
		for (k = 0; k < terms[i].n_names; k++) {
			string_free(terms[i].names[k].text);
		}
		mem_free(terms[i].names);
	}
	mem_free(terms);
	terms = NULL;
	n_terms = a_terms = 0;
	for (i = 0; i < n_manual; i++) {
		string_free(manual[i].ids);
		string_free(manual[i].text);
	}
	mem_free(manual);
	manual = NULL;
	n_manual = a_manual = 0;
	for (s = 0; s < N_ELEMENTS(series); s++) {
		for (k = 0; k < series[s].n; k++) string_free(series[s].items[k].name);
		series[s].n = 0;
	}
}

void net_glossary_write(struct net_jw *w)
{
	int i, k;
	size_t s;

	net_jw_key(w, "terms");
	if (!races || !player_abilities || !z_info) {
		net_jw_null(w);
		return;
	}
	manual_read();
	add_races_and_classes();
	add_stats();
	add_skills();
	add_abilities();
	add_elements();
	add_curses();
	add_timed();
	/* The character sheet's abilities table shows labels of
	 * PLAYER_TABLE_LABEL in ui-player.h */
	visit_ui_entry_labels(6, label_visit, NULL);

	net_jw_arr(w);
	for (i = 0; i < n_terms; i++) {
		const struct gl_term *t = &terms[i];

		net_jw_obj(w);
		net_jw_kstr(w, "id", t->id);
		net_jw_kstr(w, "kind", t->kind);
		net_jw_kstr(w, "title", t->title);
		net_jw_kbool(w, "manual", t->manual);
		net_jw_key(w, "text");
		net_jw_textblock(w, t->tb);
		net_jw_key(w, "names");
		net_jw_arr(w);
		for (k = 0; k < t->n_names; k++) {
			net_jw_obj(w);
			net_jw_kstr(w, "name", t->names[k].text);
			if (t->names[k].label) net_jw_kbool(w, "label", true);
			net_jw_obj_end(w);
		}
		net_jw_arr_end(w);
		net_jw_obj_end(w);
	}
	net_jw_arr_end(w);

	net_jw_key(w, "series");
	net_jw_arr(w);
	for (s = 0; s < N_ELEMENTS(series); s++) {
		net_jw_obj(w);
		net_jw_kstr(w, "lead", series[s].lead);
		net_jw_key(w, "items");
		net_jw_arr(w);
		for (k = 0; k < series[s].n; k++) {
			net_jw_obj(w);
			net_jw_kstr(w, "name", series[s].items[k].name);
			net_jw_kstr(w, "id", series[s].items[k].id);
			net_jw_obj_end(w);
		}
		net_jw_arr_end(w);
		net_jw_obj_end(w);
	}
	net_jw_arr_end(w);
	glossary_free();
}
