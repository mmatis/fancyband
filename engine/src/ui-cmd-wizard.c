/**
 * \file ui-cmd-wizard.c
 * \brief Debug commands whose work is mostly display
 *
 * These handlers came from cmd-wizard.c.  They draw on the text UI's
 * screen, so the text UI supplies them to the engine with
 * cmd_set_frontend_handler().  The rest of the debug commands stay in
 * cmd-wizard.c.
 *
 * Copyright (c) 2021 Eric Branlund
 * Copyright (c) 1997 Ben Harrison, James E. Wilson, Robert A. Koeneke
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
#include "cmd-core.h"
#include "cmds.h"
#include "effects.h"
#include "game-input.h"
#include "generate.h"
#include "init.h"
#include "mon-lore.h"
#include "mon-make.h"
#include "mon-util.h"
#include "obj-curse.h"
#include "obj-desc.h"
#include "obj-gear.h"
#include "obj-knowledge.h"
#include "obj-make.h"
#include "obj-pile.h"
#include "obj-power.h"
#include "obj-tval.h"
#include "obj-util.h"
#include "player-calcs.h"
#include "player-timed.h"
#include "player-util.h"
#include "project.h"
#include "target.h"
#include "trap.h"
#include "ui-cmd-wizard.h"
#include "ui-input.h"
#include "ui-map.h"
#include "ui-output.h"
#include "ui-target.h"
#include "wizard.h"


/**
 * Output part of a bitflag set in binary format.
 */
static void prt_binary(const bitflag *flags, int offset, int row, int col,
	wchar_t ch, int num)
{
	int flag;

	/* Scan the flags. */
	for (flag = FLAG_START + offset; flag < FLAG_START + offset + num; flag++) {
		if (of_has(flags, flag)) {
			Term_putch(col++, row, COLOUR_BLUE, ch);
		} else {
			Term_putch(col++, row, COLOUR_WHITE, L'-');
		}
	}
}


/**
 * Display an item's properties.
 */
static void wiz_display_item(const struct object *obj, bool all,
		const struct player *p)
{
	static const char *flagLabels[] = {
		#define OF(a, b) b,
		#include "list-object-flags.h"
		#undef OF
	};
	int j = 0, i, k, nflg;
	bitflag f[OF_SIZE];
	char buf[256];
	bool *labelsDone;

	/* Extract the flags. */
	if (all) {
		object_flags(obj, f);
	} else {
		object_flags_known(obj, f);
	}

	/* Clear screen. */
	Term_clear();

	/* Describe fully */
	object_desc(buf, sizeof(buf), obj,
		ODESC_PREFIX | ODESC_FULL | ODESC_SPOIL, p);

	prt(buf, 2, j);

	prt(format("combat = (%dd%d) (%+d,%+d) [%d,%+d]",
		obj->dd, obj->ds, object_to_hit(all ? obj : obj->known),
		object_to_dam(all ? obj : obj->known), obj->ac,
		object_to_ac(all ? obj : obj->known)), 4, j);

	prt(format("kind = %-5lu  tval = %-5d  sval = %-5d  wgt = %-3d     timeout = %-d",
		(unsigned long)obj->kind->kidx, obj->tval, obj->sval,
		object_weight_one(obj), obj->timeout), 5, j);

	prt(format("number = %-3d  pval = %-5d  name1 = %-4ld  egoidx = %-4ld  cost = %ld",
		obj->number, obj->pval,
		obj->artifact ? (long)obj->artifact->aidx : 0L,
		obj->ego ? (long)obj->ego->eidx : -1L,
		(long)object_value(obj, 1)), 6, j);

	nflg = MIN(OF_MAX - FLAG_START, 80);

	/* Set up header line. */
	if (nflg >= 6) {
		buf[0] = '+';
		k = (nflg - 6) / 2;
		for (i = 1; i < k; ++i) {
			buf[i] = '-';
		}
	} else {
		k = 0;
	}
	buf[k] = 'F';
	buf[k + 1] = 'L';
	buf[k + 2] = 'A';
	buf[k + 3] = 'G';
	buf[k + 4] = 'S';
	for (i = k + 5; i < nflg - 1; ++i) {
		buf[i] = '-';
	}
	if (nflg >= 7) {
		buf[nflg - 1] = '+';
		buf[nflg] = '\0';
	} else {
		buf[k + 5] = '\0';
	}
	prt(buf, 16, j);

	/* Display first five letters of flag labels vertically. */
	labelsDone = mem_zalloc(nflg * sizeof(*labelsDone));
	for (k = 0; k < 5; ++k) {
		for (i = 0; i < nflg; ++i) {
			if (labelsDone[i]) {
				buf[i] = ' ';
			} else if (flagLabels[i][k] == '\0') {
				labelsDone[i] = true;
				buf[i] = ' ';
			} else {
				buf[i] = flagLabels[i][k];
			}
		}
		buf[nflg] = '\0';
		prt(buf, 17 + k, j);
	}
	mem_free(labelsDone);

	prt_binary(f, 0, 22, j, L'*', nflg);
	if (obj->known) {
		prt_binary(obj->known->flags, 0, 23, j, L'+', nflg);
	}
}


/**
 * Redraw the visible portion of the map to accentuate some chosen
 * characteristic.
 *
 * \param c is the chunk to use as the source for data.
 * \param p is the player to use.
 * \param func is a pointer to a function which will set the value pointed
 * to by its fourth argument to whether or not to display the given grid and,
 * if displaying that grid, set its fifth argument to the color to use for the
 * grid.
 * \param closure is passed as the second argument to func.
 *
 * Assumes the active terminal displays a map.
 */
static void wiz_hack_map(struct chunk *c, struct player *p,
	void (*func)(struct chunk *, void *, struct loc, bool *, uint8_t *),
	void *closure)
{
	int y;

	for (y = Term->offset_y; y < Term->offset_y + SCREEN_HGT; y++) {
		int x;

		for (x = Term->offset_x; x < Term->offset_x + SCREEN_WID; x++) {
			struct loc grid = loc(x, y);
			bool show;
			uint8_t color;

			if (!square_in_bounds_fully(c, grid)) continue;

			(*func)(c, closure, grid, &show, &color);
			if (!show) continue;

			if (loc_eq(grid, p->grid)) {
				print_rel(L'@', color, y, x);
			} else if (square_ispassable(c, grid)) {
				print_rel(L'*', color, y, x);
			} else {
				print_rel(L'#', color, y, x);
			}
		}
	}
}


/**
 * Display the keycodes the user has been generating (CMD_WIZ_DISPLAY_KEYLOG).
 * Takes no arguments from cmd.
 */
void do_cmd_wiz_display_keylog(struct command *cmd)
{
	int i;
	char buf[50];
	char buf2[12];
	struct keypress keys[2] = {KEYPRESS_NULL, KEYPRESS_NULL};

	screen_save();

	prt("Previous keypresses (top most recent):", 0, 0);

	for (i = 0; i < KEYLOG_SIZE; i++) {
		if (i < log_size) {
			/*
			 * Find the keypress from the log; log_i is one past
			 * the most recent.
			 */
			int j = (log_i > i) ?
				log_i - i - 1 : log_i - i - 1 + KEYLOG_SIZE;
			struct keypress k = keylog[j];

			/*
			 * ugh. it would be nice if there was a version of
			 * keypress_to_text which took only one keypress.
			 */
			keys[0] = k;
			keypress_to_text(buf2, sizeof(buf2), keys, true);

			/* format this line of output */
			strnfmt(buf, sizeof(buf), "    %-12s (code=%lu mods=%u)",
				buf2, (unsigned long)k.code, k.mods);
		} else {
			/* create a blank line of output */
			strnfmt(buf, sizeof(buf), "%40s", "");
		}

		prt(buf, i + 1, 0);
	}

	prt("Press any key to continue.", KEYLOG_SIZE + 1, 0);
	anykey();
	screen_load();
}


/**
 * Dump a map of the current level as an HTML file (CMD_WIZ_DUMP_LEVEL_MAP).
 * Takes no arguments from cmd.
 *
 * Bugs:
 * The path and title could be passed through arguments of type string on the
 * command.  That, however, is problematic because there's nothing to say how
 * the lifetime of the string should be handled (does it need to be freed at
 * all, should it be freed here which would break the option to repeat the
 * command, or should it be freed within cmd-core.c as part of managing the
 * lifecycle for the command).
 */
void do_cmd_wiz_dump_level_map(struct command *cmd)
{
	char path[1024] = "";
	char title[80];
	ang_file *fo;

	strnfmt(title, sizeof(title), "Map of level %d", player->depth);
	if (!get_file("level.html", path, sizeof(path)) ||
			!get_string("Title for map: ", title, sizeof(title))) {
		return;
	}
	fo = file_open(path, MODE_WRITE, FTYPE_TEXT);
	if (fo) {
		dump_level(fo, title, cave, NULL);
		if (file_close(fo)) {
			msg("Level dumped to %s.", path);
		}
	}
}


/**
 * Is a helper function passed by do_cmd_wiz_peek_noise_scent() to
 * wiz_hack_map() in order to peek at the noise.
 *
 * \param c is the chunk to access for data.
 * \param closure is a pointer to an integer with the desired noise level.
 * \param grid is the location in the chunk.
 * \param show is dereferenced and set to true if grid has the desired noise.
 * Otherwise, it is dereferenced and set to false.
 * \param color is dereferenced and set to the color to use if *show is set
 * to true.  Otherwise, it is not dereferenced.
 */
static void wiz_hack_map_peek_noise(struct chunk *c, void *closure,
	struct loc grid, bool *show, uint8_t *color)
{
	if (c->noise.grids[grid.y][grid.x] == *((int*)closure)) {
		*show = true;
		*color = COLOUR_RED;
	} else {
		*show = false;
	}
}


/**
 * Is a helper function passed by do_cmd_wiz_peek_noise_scent() to
 * wiz_hack_map() in order to peek at the scent.
 *
 * \param c is the chunk to access for data.
 * \param closure is a pointer to an integer with the desired scent level.
 * \param grid is the location in the chunk.
 * \param show is dereferenced and set to true if grid has the desired scent.
 * Otherwise, it is dereferenced and set to false.
 * \param color is dereferenced and set to the color to use if *show is set
 * to true.  Otherwise, it is not dereferenced.
 */
static void wiz_hack_map_peek_scent(struct chunk *c, void *closure,
	struct loc grid, bool *show, uint8_t *color)
{
	if (c->scent.grids[grid.y][grid.x] == *((int*)closure)) {
		*show = true;
		*color = COLOUR_YELLOW;
	} else {
		*show = false;
	}
}


/**
 * Display in sequence the squares at n grids from the player, as measured by
 * the noise and scent algorithms; n goes from 1 to the maximum flow depth
 * (CMD_WIZ_PEEK_NOISE_SCENT).  Takes no arguments from cmd.
 */
void do_cmd_wiz_peek_noise_scent(struct command *cmd)
{
	int i;
	char kp;

	/* Noise */
	for (i = 0; i < 100; i++) {
		wiz_hack_map(cave, player, wiz_hack_map_peek_noise, &i);

		/* Get key */
		if (!get_com(format("Depth %d: ", i), &kp)) break;

		/* Redraw map */
		prt_map();
	}

	/* Smell */
	for (i = 0; i < 50; i++) {
		wiz_hack_map(cave, player, wiz_hack_map_peek_scent, &i);

		/* Get key */
		if (!get_com(format("Depth %d: ", i), &kp)) break;

		/* Redraw map */
		prt_map();
	}

	/* Done */
	prt("", 0, 0);

	/* Redraw map */
	prt_map();
}


/**
 * Perform an effect (CMD_WIZ_PERFORM_EFFECT).  Takes no arguments from cmd.
 *
 * Bugs:
 * If the command is repeated, it prompts again for all the effect's parameters.
 * The number of parameters currently exceeds CMD_MAX_ARGS so a one-to-one
 * mapping for storing the parameters as arguments in the command would require
 * increasing CMD_MAX_ARGS.  Otherwise, the parameters would have to be
 * multiplexed into the available arguments to store them in the command.  The
 * handling of the lifetime of string arguments for commands is also awkward
 * which would also hamper storing the effect's parameters in the command.
 */
void do_cmd_wiz_perform_effect(struct command *cmd)
{
	char name[80] = "";
	char dice[80] = "0";
	int index = -1;
	int p1 = 0, p2 = 0, p3 = 0;
	int y = 0, x = 0;
	bool ident = false;

	/* Avoid the prompt getting in the way */
	screen_save();

	/* Get the name */
	if (get_string("Do which effect: ", name, sizeof(name))) {
		/* See if an effect index was entered */
		if (!get_int_from_string(name, &index)) {
			/* If not, find the effect with that name */
			index = effect_lookup(name);
		}

		/* Failed */
		if (index <= EF_NONE || index >= EF_MAX) {
			msg("No effect found.");
			return;
		}
	}

	/* Get the dice */
	if (! get_string("Enter damage dice (eg 1+2d6M2): ", dice,
			sizeof(dice))) {
		my_strcpy(dice, "0", sizeof(dice));
	}

	/* Get the effect subtype */
	my_strcpy(name, "0", sizeof(name));
	if (get_string("Enter name or number for effect subtype: ", name,
			sizeof(name))) {
		/* See if an effect parameter was entered */
		p1 = effect_subtype(index, name);
		if (p1 == -1) p1 = 0;
	}

	/* Get the parameters */
	p2 = get_quantity("Enter second parameter (radius): ", 100);
	p3 = get_quantity("Enter third parameter (other): ", 100);
	y = get_quantity("Enter y parameter: ", 100);
	x = get_quantity("Enter x parameter: ", 100);

	/* Reload the screen */
	screen_load();

	effect_simple(index, source_player(), dice, p1, p2, p3, y, x, &ident);

	if (ident) {
		msg("Identified!");
	}
}


/**
 * Play with an item (CMD_WIZ_PLAY_ITEM).  Can take the item to play with
 * from the argument, "item", of type item in cmd.  Uses the arguments,
 * "original_item" (of type item), "all_prop" (of type choice), and "changed"
 * (of type choice), internally for a session of playing with the same object.
 *
 * Things that can be done are:
 * - output statistics via CMD_WIZ_STAT_ITEM
 * - reroll item via CMD_WIZ_REROLL_ITEM
 * - change properties via CMD_WIZ_TWEAK_ITEM
 * - change a curse via CMD_WIZ_CURSE_ITEM
 * - change the number of items via CMD_WIZ_CHANGE_ITEM_QUANTITY
 */
void do_cmd_wiz_play_item(struct command *cmd)
{
	struct object *obj = NULL;
	struct object *orig_obj = NULL;
	int display_all_prop = 1;
	int object_changed = 0;
	bool done = false;
	bool rejected = true;
	const char *done_msg = NULL;
	char ch;

	/*
	 * When called to initiate a play session, "item" may be set, but
	 * "original_item" should not be set or should be set to NULL.
	 * Subsequent calls in the same session will have both set to
	 * non-NULL vales along with "all_prop" and "changed".
	 */
	if (cmd_get_arg_item(cmd, "original_item", &orig_obj) == CMD_OK &&
			orig_obj) {
		if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK || !obj) {
			assert(0);
			return;
		}
		if (cmd_get_arg_choice(cmd, "all_prop", &display_all_prop) !=
				CMD_OK) {
			assert(0);
			return;
		}
		if (cmd_get_arg_choice(cmd, "changed", &object_changed) !=
				CMD_OK) {
			assert(0);
			return;
		}
	} else {
		if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK || !obj) {
			if (!get_item(&obj, "Play with which object? ",
					"You have nothing to play with.",
					cmd->code, NULL, (USE_EQUIP |
					USE_INVEN | USE_QUIVER | USE_FLOOR))) {
				return;
			}
		}

		/* Remember the object. */
		cmd_set_arg_item(cmd, "item", obj);

		/* Make a copy so changes can be rejected. */
		orig_obj = object_new();
		object_copy(orig_obj, obj);
		cmd_set_arg_item(cmd, "original_item", orig_obj);

		/*
		 * Store whether all properties are shown or only the known
		 * ones.
		 */
		if (cmd_get_arg_choice(cmd, "all_prop", &display_all_prop) !=
				CMD_OK) {
			display_all_prop = 1;
			cmd_set_arg_choice(cmd, "all_prop", display_all_prop);
		}

		/*
		 * Store whether the working item may have changed from the
		 * original.
		 */
		if (cmd_get_arg_choice(cmd, "changed", &object_changed) !=
				CMD_OK) {
			object_changed = 0;
			cmd_set_arg_choice(cmd, "changed", object_changed);
		}

		/* Save screen. */
		screen_save();
	}

	/* Display the (possibly modified) item. */
	wiz_display_item(obj, display_all_prop != 0, player);

	/* Get choice. */
	if (get_com("[a]ccept [s]tatistics [r]eroll [t]weak [c]urse [q]uantity [k]nown? ", &ch)) {
		bool queue_failed = false;

		switch (ch) {
			case 'A':
			case 'a':
				/* Accept whatever changes were made. */
				done = true;
				rejected = false;
				if (object_changed) {
					/* Mark for updates. */
					if (object_is_carried(player, obj)
							&& (obj->number !=
							orig_obj->number
							|| object_weight_one(obj)
							!= object_weight_one(orig_obj))) {
						/*
						 * Remove the weight of the old
						 * version.
						 */
						player->upkeep->total_weight -=
							orig_obj->number
							* object_weight_one(orig_obj);

						/*
						 * Add the weight of the new
						 * version.
						 */
						player->upkeep->total_weight +=
							obj->number
							* object_weight_one(obj);
					}
					object_touch(player, obj);
					if (object_is_equipped(player->body, obj)) {
						assert(obj->known);
						obj->known->notice &=
							~OBJ_NOTICE_WORN;
						object_learn_on_wield(player,
							obj);
					}
					wiz_play_item_standard_upkeep(player,
						obj);
				}
				break;

			case 'C':
			case 'c':
				/* Change a curse on the item. */
				if (cmdq_push(CMD_WIZ_CURSE_ITEM) == 0) {
					cmd_set_arg_item(cmdq_peek(), "item",
						obj);
					cmd_set_arg_choice(cmdq_peek(),
						"update", 0);
				} else {
					queue_failed = true;
				}
				break;

			case 'S':
			case 's':
				/* Get statistics about the item. */
				if (cmdq_push(CMD_WIZ_STAT_ITEM) == 0) {
					cmd_set_arg_item(cmdq_peek(), "item",
						obj);
				} else {
					queue_failed = true;
				}
				break;

			case 'R':
			case 'r':
				/* Reroll the item. */
				if (cmdq_push(CMD_WIZ_REROLL_ITEM) == 0) {
					cmd_set_arg_item(cmdq_peek(), "item",
						obj);
					cmd_set_arg_choice(cmdq_peek(),
						"update", 0);
				} else {
					queue_failed = true;
				}
				break;

			case 'T':
			case 't':
				/* Tweak the object's properties. */
				if (cmdq_push(CMD_WIZ_TWEAK_ITEM) == 0) {
					cmd_set_arg_item(cmdq_peek(), "item",
						obj);
					cmd_set_arg_choice(cmdq_peek(),
						"update", 0);
				} else {
					queue_failed = true;
				}
				break;

			case 'K':
			case 'k':
				/* Toggle whether showing all properties. */
				display_all_prop = !display_all_prop;
				cmd_set_arg_choice(cmd, "all_prop",
					display_all_prop);
				break;

			case 'Q':
			case 'q':
				/* Change the number of items in the stack. */
				if (cmdq_push(CMD_WIZ_CHANGE_ITEM_QUANTITY) == 0) {
					cmd_set_arg_item(cmdq_peek(), "item",
						obj);
					cmd_set_arg_choice(cmdq_peek(),
						"update", 0);
				} else {
					queue_failed = true;
				}
				break;

			default:
				/*
				 * Don't have to do anything, next pass through
				 * will ask again what's wanted.
				 */
				break;
		}

		if (queue_failed &&
				get_check("Couldn't proceed.  Stop playing with item and lose all changes? ")) {
			done = true;
			if (object_changed) {
				done_msg = "Bailed out.  Changes to item lost.";
			}
		}
	} else {
		done = true;
		if (object_changed) {
			done_msg = "Changes ignored.";
		}
	}

	if (!done) {
		/* Push the command back on the queue to be reexecuted. */
		if (cmdq_push_copy(cmd) != 0) {
			/* Failed.  Bail out without saving changes. */
			done = true;
			done_msg = "Couldn't queue command.  Changes lost.";
		}
	}

	if (done) {
		if (rejected && object_changed) {
			/*
			 * Restore to the original values.  The pile links
			 * require special handling because object_copy()
			 * resets them.
			 */
			struct object *prev = obj->prev;
			struct object *next = obj->next;

			/* Free slays, brands, and curses by hand. */
			mem_free(obj->slays);
			obj->slays = NULL;
			mem_free(obj->brands);
			obj->brands = NULL;
			mem_free(obj->curses);
			obj->curses = NULL;

			object_copy(obj, orig_obj);
			obj->prev = prev;
			obj->next = next;
		}

		/* Release the preserved copy. */
		object_delete(cave, player->cave, &orig_obj);

		/*
		 * Reset the original_item and changed arguments so repeating
		 * the command will start a new play session without a
		 * dangling reference to the deleted preserved copy.
		 */
		cmd_set_arg_item(cmd, "original_item", NULL);
		cmd_set_arg_choice(cmd, "changed", 0);

		/* Restore the screen. */
		screen_load();

		/* Provide some feedback. */
		if (done_msg) {
			msg("%s", done_msg);
		}
	}
}


struct wiz_query_feature_closure {
	const int *features;
	int n;
};


/**
 * Is a helper function passed by do_cmd_wiz_query_feature() to wiz_hack_map().
 *
 * \param c is the chunk to access for data.
 * \param closure is a pointer to a struct wiz_query_feature_closure cast to a
 * pointer to void.  Selects the terrain shown.
 * \param grid is the location in the chunk.
 * \param show is dereferenced and set to true if grid contains one of the
 * types of terrain selected by closure.  Otherwise, it is dereferenced and
 * set to false.
 * \param color is dereferenced and set to the color to use if *show is set
 * to true.  Otherwise, it is not dereferenced.
 */
static void wiz_hack_map_query_feature(struct chunk *c, void *closure,
	struct loc grid, bool *show, uint8_t *color)
{
	const struct wiz_query_feature_closure *sel_feats = closure;
	int i = 0;
	int sq_feat = square(c, grid)->feat;

	while (1) {
		if (i >= sel_feats->n) {
			*show = false;
			return;
		}
		if (sq_feat == sel_feats->features[i]) {
			*show = true;
			*color = (square_ispassable(c, grid)) ?
				COLOUR_YELLOW : COLOUR_RED;
			return;
		}
		++i;
	}
}


/**
 * Redraw the visible portion of the map to highlight certain terrain
 * (CMD_WIZ_QUERY_FEATURE).  Can take the terrain to highlight from the
 * argument, "choice", of type choice in cmd.  This function will need
 * to be changed if the terrain types change.
 */
void do_cmd_wiz_query_feature(struct command *cmd)
{
	int feature_class;
	struct wiz_query_feature_closure selected;
	/* OMG hax */
	const int featf[] = { FEAT_FLOOR };
	const int feato[] = { FEAT_OPEN };
	const int featb[] = { FEAT_BROKEN };
	const int featu[] = { FEAT_LESS };
	const int featz[] = { FEAT_MORE };
	const int featt[] = { FEAT_LESS, FEAT_MORE };
	const int featc[] = { FEAT_CLOSED };
	const int featd[] = { FEAT_CLOSED, FEAT_OPEN, FEAT_BROKEN,
		FEAT_SECRET };
	const int feath[] = { FEAT_SECRET };
	const int featm[] = { FEAT_MAGMA, FEAT_MAGMA_K };
	const int featq[] = { FEAT_QUARTZ, FEAT_QUARTZ_K };
	const int featg[] = { FEAT_GRANITE };
	const int featp[] = { FEAT_PERM };
	const int featr[] = { FEAT_RUBBLE };
	const int feata[] = { FEAT_PASS_RUBBLE };

	if (cmd_get_arg_choice(cmd, "choice", &feature_class) != CMD_OK) {
		char choice;

		if (!get_com("Debug Command Feature Query: ", &choice)) return;
		feature_class = choice;
		cmd_set_arg_choice(cmd, "choice", feature_class);
	}

	switch (feature_class) {
		/* Floors */
		case 'f':
			selected.features = featf;
			selected.n = (int) N_ELEMENTS(featf);
			break;

		/* Open doors */
		case 'o':
			selected.features = feato;
			selected.n = (int) N_ELEMENTS(feato);
			break;

		/* Broken doors */
		case 'b':
			selected.features = featb;
			selected.n = (int) N_ELEMENTS(featb);
			break;

		/* Upstairs */
		case 'u':
			selected.features = featu;
			selected.n = (int) N_ELEMENTS(featu);
			break;

		/* Downstairs */
		case 'z':
			selected.features = featz;
			selected.n = (int) N_ELEMENTS(featz);
			break;

		/* Stairs */
		case 't':
			selected.features = featt;
			selected.n = (int) N_ELEMENTS(featt);
			break;

		/* Closed doors */
		case 'c':
			selected.features = featc;
			selected.n = (int) N_ELEMENTS(featc);
			break;

		/* Doors */
		case 'd':
			selected.features = featd;
			selected.n = (int) N_ELEMENTS(featd);
			break;

		/* Secret doors */
		case 'h':
			selected.features = feath;
			selected.n = (int) N_ELEMENTS(feath);
			break;

		/* Magma */
		case 'm':
			selected.features = featm;
			selected.n = (int) N_ELEMENTS(featm);
			break;

		/* Quartz */
		case 'q':
			selected.features = featq;
			selected.n = (int) N_ELEMENTS(featq);
			break;

		/* Granite */
		case 'g':
			selected.features = featg;
			selected.n = (int) N_ELEMENTS(featg);
			break;

		/* Permanent wall */
		case 'p':
			selected.features = featp;
			selected.n = (int) N_ELEMENTS(featp);
			break;

		/* Rubble */
		case 'r':
			selected.features = featr;
			selected.n = (int) N_ELEMENTS(featr);
			break;

		/* Passable rubble */
		case 'a':
			selected.features = feata;
			selected.n = (int) N_ELEMENTS(feata);
			break;

		/* Invalid entry */
		default:
			msg("That was an invalid selection.  Use one of fobuztcdhmqgpra .");
			return;
	}

	wiz_hack_map(cave, player, wiz_hack_map_query_feature, &selected);

	Term_redraw();

	msg("Press any key.");
	inkey_ex();
	prt("", 0, 0);

	/* Redraw map */
	prt_map();
}


/**
 * Is a helper function passed by do_cmd_wiz_query_square_flag() to
 * wiz_hack_map().
 *
 * \param c is the chunk to access for data.
 * \param closure is a pointer to the int with the flag to highlight.
 * \param grid is the location in the chunk.
 * \param show is dereferenced and set to true if the grid has the flag.
 * Otherwise, it is dereferenced and set to false.
 * \param color is dereferenced and set to the color to use if *show is set
 * to true.  Otherwise, it is not dereferenced.
 */
static void wiz_hack_map_query_square_flag(struct chunk *c, void *closure,
	struct loc grid, bool *show, uint8_t *color)
{
	int flag = *((int*)closure);

	/* With a flag, test for that.  Otherwise, test if grid is known. */
	if ((flag && sqinfo_has(square(c, grid)->info, flag)) ||
			(!flag && square_isknown(c, grid))) {
		*show = true;
		*color = (square_ispassable(c, grid)) ?
			COLOUR_YELLOW : COLOUR_RED;
	} else {
		*show = false;
	}
}


/**
 * Redraw the visible portion of the map to highlight squares with a given
 * flag (CMD_WIZ_QUERY_SQUARE_FLAG).  Can take the flag to highlight from the
 * argument, "choice", of type choice in cmd.  This function will need to
 * be changed if list-square-flags.h changes.
 */
void do_cmd_wiz_query_square_flag(struct command *cmd)
{
	int flag = 0;

	if (cmd_get_arg_choice(cmd, "choice", &flag) != CMD_OK) {
		char c;

		if (!get_com("Debug Command Query [grasvwdftniolx]: ", &c))
			return;
		switch (c) {
			case 'g': flag = SQUARE_GLOW; break;
			case 'r': flag = SQUARE_ROOM; break;
			case 'a': flag = SQUARE_VAULT; break;
			case 's': flag = SQUARE_SEEN; break;
			case 'v': flag = SQUARE_VIEW; break;
			case 'w': flag = SQUARE_WASSEEN; break;
			case 'd': flag = SQUARE_DTRAP; break;
			case 'f': flag = SQUARE_FEEL; break;
			case 't': flag = SQUARE_TRAP; break;
			case 'n': flag = SQUARE_INVIS; break;
			case 'i': flag = SQUARE_WALL_INNER; break;
			case 'o': flag = SQUARE_WALL_OUTER; break;
			case 'l': flag = SQUARE_WALL_SOLID; break;
			case 'x': flag = SQUARE_MON_RESTRICT; break;
		}
		cmd_set_arg_choice(cmd, "choice", flag);
	}

	wiz_hack_map(cave, player, wiz_hack_map_query_square_flag, &flag);

	Term_redraw();

	msg("Press any key.");
	inkey_ex();
	prt("", 0, 0);

	/* Redraw map */
	prt_map();
}


/**
 * Get some statistics about the rarity of an item (CMD_WIZ_STAT_ITEM).  Can
 * take the item to use from the argument, "item", of type item in cmd.  Can
 * take how items used for comparison are rolled from the argument, "choice",
 * of type choice in cmd:  a value of zero means calling make_object() with
 * good and great set to false, a value of one means calling make_object()
 * with good set to true and great set to false, and a value of two means
 * calling make_object() with good and great set to true.  Can take the
 * depth used when generating items from the argument, "depth", of type
 * number in cmd.
 *
 * Generate a lot of fake items and see if they are of the same type (tval and
 * sval).  Then compare modifiers, AC, to-hit, and to-dam to get whether the
 * fake item matches (all modifiers are the same as are the AC, to-hit, and
 * to-dam), is better (all modifiers, AC, to-hit, and to-dam are not less
 * than on the target item and at least one of those is more than the matching
 * attribute on the target item), is worse (all modifiers, AC, to-hit, and
 * to-dam are not more than on the target item and at least one of those is
 * less than the matching attribute on the target item), or is different.
 * Curses, brands, slays, and other properties not treated as modifiers have
 * no effect on that classification.  A comment left about this procedure in
 * wiz-debug.c was "HINT: This is *very* useful for balancing the game!".
 */
/* This is the maximum number of rolls to use. */
#define TEST_ROLL 100000
void do_cmd_wiz_stat_item(struct command *cmd)
{
	const char *repfmt =
		"Rolls: %ld, Matches: %ld, Better: %ld, Worse: %ld, Other: %ld";
	int level = player->depth;
	int treasure_choice = 0;
	bool good = false, great = false;
	long matches = 0, better = 0, worse = 0, other = 0;
	long n = TEST_ROLL;
	struct object *obj;
	long i;
	const char *quality;

	/* Get the target item for comparison. */
	if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK) {
		if (!get_item(&obj, "Compare with which item? ",
				"You have nothing to compare.", cmd->code,
				NULL, (USE_EQUIP | USE_INVEN | USE_QUIVER |
				USE_FLOOR))) {
			return;
		}
		cmd_set_arg_item(cmd, "item", obj);
	}

	/* Display item. */
	wiz_display_item(obj, true, player);

	/* Get what kind of treasure to generate. */
	if (cmd_get_arg_choice(cmd, "choice", &treasure_choice) != CMD_OK) {
		char ch;

		if (!get_com("Roll for [n]ormal, [g]ood, or [e]xcellent treasure? ", &ch)) {
			return;
		}
		if (ch == 'n' || ch == 'N') {
			treasure_choice = 0;
		} else if (ch == 'g' || ch == 'G') {
			treasure_choice = 1;
		} else if (ch == 'e' || ch == 'E') {
			treasure_choice = 2;
		} else {
			return;
		}
		cmd_set_arg_choice(cmd, "choice", treasure_choice);
	}
	if (treasure_choice == 0) {
		good = false;
		great = false;
		quality = "normal";
	} else if (treasure_choice == 1) {
		good = true;
		great = false;
		quality = "good";
	} else if (treasure_choice == 2) {
		good = true;
		great = true;
		quality = "excellent";
	} else {
		return;
	}

	/* Get the depth to use when generating treasure. */
	if (cmd_get_arg_number(cmd, "depth", &level) != CMD_OK) {
		char prompt[80], s[80];

		strnfmt(prompt, sizeof(prompt), "Depth for treasure (0-%d): ",
			z_info->max_depth - 1);

		/* Set default. */
		strnfmt(s, sizeof(s), "%d", player->depth);

		if (!get_string(prompt, s, sizeof(s)) ||
				!get_int_from_string(s, &level) || level < 0 ||
				level >= z_info->max_depth) {
			return;
		}
		cmd_set_arg_number(cmd, "depth", level);
	}
	if (level < 0 || level >= z_info->max_depth) {
		return;
	}

	/* Give feedback about what's going to be done. */
	msg("Creating a lot of %s items.  Base level = %d.", quality, level);
	event_signal(EVENT_MESSAGE_FLUSH);

	for (i = 0; i < n; i++) {
		bool ismatch = true, isbetter = true, isworse = true;
		struct object *test_obj;
		int j;

		/* Output every few rolls. */
		if (i < 100 || i % 100 == 0) {
			ui_event e;

			/* Do not wait. */
			inkey_scan = SCAN_INSTANT;

			/* Allow interrupt */
			e = inkey_ex();
			if (e.type != EVT_NONE) {
				event_signal(EVENT_INPUT_FLUSH);
				break;
			}

			/* Dump the stats. */
			prt(format(repfmt, i, matches, better, worse, other),
				0, 0);
			Term_fresh();
		}

		/* Create an object. */
		test_obj = make_object(cave, level, good, great, false, NULL, 0);

		/*
		 * Allow multiple artifacts, because breaking the game is OK
		 * here.
		 */
		if (obj->artifact) {
			mark_artifact_created(obj->artifact, false);
		}

		/* Check for failures to generate an object. */
		if (!test_obj) continue;

		/* Test for same tval and sval. */
		if (obj->tval != test_obj->tval ||
				obj->sval != test_obj->sval) {
			object_delete(cave, player->cave, &test_obj);
			continue;
		}

		/* Check the modifiers. */
		for (j = 0; j < OBJ_MOD_MAX; j++) {
			if (test_obj->modifiers[j] != obj->modifiers[j]) {
				ismatch = false;
				if (test_obj->modifiers[j] < obj->modifiers[j]) {
					isbetter = false;
				} else {
					isworse = false;
				}
			}
		}

		/* Check for match over all the tested properties. */
		if (ismatch && test_obj->to_a == obj->to_a &&
				test_obj->to_h == obj->to_h &&
				test_obj->to_d == obj->to_d) {
			++matches;
		/* Check for same or better over all the tested properties. */
		} else if (isbetter && test_obj->to_a >= obj->to_a &&
				test_obj->to_h >= obj->to_h &&
				test_obj->to_d >= obj->to_d) {
			++better;
		/* Check for same or worse over all the tested properties. */
		} else if (isworse && test_obj->to_a <= obj->to_a &&
				test_obj->to_h <= obj->to_h &&
				test_obj->to_d <= obj->to_d) {
			++worse;
		/* Everything else is merely different. */
		} else {
			++other;
		}

		/* Nuke the test object. */
		object_delete(cave, player->cave, &test_obj);
	}

	/* Final dump */
	msg(repfmt, i, matches, better, worse, other);
	event_signal(EVENT_MESSAGE_FLUSH);

	/* Normally only make a single artifact */
	if (obj->artifact) {
		mark_artifact_created(obj->artifact, true);
	}
}


/**
 * Give the engine the debug commands above, and the item display that
 * do_cmd_wiz_tweak_item() uses.  Safe to call more than once.
 */
void textui_register_wizard_commands(void)
{
	(void) cmd_set_frontend_handler(CMD_WIZ_DISPLAY_KEYLOG,
		do_cmd_wiz_display_keylog);
	(void) cmd_set_frontend_handler(CMD_WIZ_DUMP_LEVEL_MAP,
		do_cmd_wiz_dump_level_map);
	(void) cmd_set_frontend_handler(CMD_WIZ_PEEK_NOISE_SCENT,
		do_cmd_wiz_peek_noise_scent);
	(void) cmd_set_frontend_handler(CMD_WIZ_PERFORM_EFFECT,
		do_cmd_wiz_perform_effect);
	(void) cmd_set_frontend_handler(CMD_WIZ_PLAY_ITEM,
		do_cmd_wiz_play_item);
	(void) cmd_set_frontend_handler(CMD_WIZ_QUERY_FEATURE,
		do_cmd_wiz_query_feature);
	(void) cmd_set_frontend_handler(CMD_WIZ_QUERY_SQUARE_FLAG,
		do_cmd_wiz_query_square_flag);
	(void) cmd_set_frontend_handler(CMD_WIZ_STAT_ITEM,
		do_cmd_wiz_stat_item);
	wiz_display_item_hook = wiz_display_item;
}
