/**
 * \file ui-knowledge.h
 * \brief Player knowledge functions
 *
 * Copyright (c) 2000-2007 Eytan Zweig, Andrew Doull, Pete Mack.
 * Copyright (c) 2010 Peter Denison, Chris Carr.
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
#ifndef UI_KNOWLEDGE_H
#define UI_KNOWLEDGE_H

#include "datafile.h"
#include "monster.h"
#include "z-textblock.h"

void textui_browse_object_knowledge(const char *name, int row);
void textui_knowledge_init(void);
void textui_knowledge_cleanup(void);
void textui_browse_knowledge(void);
void do_cmd_message_one(void);
void do_cmd_messages(void);
void do_cmd_inven(void);
void do_cmd_equip(void);
void do_cmd_quiver(void);
void do_cmd_look(void);
void do_cmd_locate(void);
int cmp_monsters(const void *a, const void *b);
void do_cmd_query_symbol(void);
void do_cmd_center_map(void);
void do_cmd_monlist(void);
void do_cmd_itemlist(void);

/*
 * The knowledge menu's lists, for front ends that draw their own
 * knowledge screens.  Each list holds what the game's own screen for
 * that section shows, in its order and groups.
 */
enum knowledge_section {
	KNOW_OBJECTS,
	KNOW_RUNES,
	KNOW_ARTIFACTS,
	KNOW_EGOS,
	KNOW_MONSTERS,
	KNOW_FEATURES,
	KNOW_TRAPS,
	KNOW_SHAPES,
	KNOW_SECTIONS
};

struct knowledge_entry {
	/* The name of the entry's group */
	const char *group;
	/* What knowledge_lore() takes: a monster race's, object kind's,
	 * artifact's, ego item's, rune's, feature's, trap kind's, or
	 * shape's index */
	int id;
	char name[120];
	/* A short note, such as a monster's kills or an inscription */
	char note[40];
	/* The entry is not fully known, as an unaware flavor or an ego item
	 * not seen yet */
	bool dim;
	/* Whether attr and ch hold how the map shows the entry, a tile
	 * when attr has its high bit set, and d_attr and d_char its own
	 * letter */
	bool glyph;
	uint8_t attr, d_attr;
	wchar_t ch, d_char;
};

typedef void (*knowledge_entry_fn)(const struct knowledge_entry *entry,
	void *data);

const char *knowledge_section_title(enum knowledge_section section);
bool knowledge_section_known(enum knowledge_section section);
int knowledge_list(enum knowledge_section section, knowledge_entry_fn fn,
	void *data);
textblock *knowledge_lore(enum knowledge_section section, int id,
	char *title, size_t len);

/* Exposed for use by test cases. */
struct ui_monster_category {
	struct ui_monster_category *next;
	const char *name;
	const struct monster_base **inc_bases;
	bitflag inc_flags[RF_SIZE];
	int n_inc_bases, max_inc_bases;
};
struct ui_knowledge_parse_state {
	struct ui_monster_category *categories;
};
extern struct file_parser ui_knowledge_parser;

#endif /* UI_KNOWLEDGE_H */
