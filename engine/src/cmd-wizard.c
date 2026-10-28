/**
 * \file cmd-wizard.c
 * \brief Implements debug commands in Angband 4's command system.
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
#include "wizard.h"


/*
 * Used by do_cmd_wiz_edit_player_*() functions to track the state of the
 * editing process.
 */
enum EditPlayerState {
	EDIT_PLAYER_UNKNOWN, EDIT_PLAYER_STARTED, EDIT_PLAYER_BREAK
} edit_player_state = EDIT_PLAYER_UNKNOWN;


/**
 * Extract a decimal integer from a string while ensuring that only the
 * decimal value and white space are present in the string.
 * \param s is the string to parse.
 * \param val is dereferenced and set to the extracted value.
 * \return true if val was dereferenced and set; false if the extraction
 * failed
 */
bool get_int_from_string(const char *s, int *val)
{
	char *endptr;
	long lval = strtol(s, &endptr, 10);

	/*
	 * Reject INT_MIN and INT_MAX so, for systems where
	 * sizeof(long) == sizeof(int), it isn't necessary to check errno to
	 * see if the value in the string was out of range.
	 */
	if (s[0] == '\0' ||
			(*endptr != '\0' && !contains_only_spaces(endptr)) ||
			lval <= INT_MIN || lval >= INT_MAX) {
		return false;
	}
	*val = (int)lval;
	return true;
}


/**
 * Extract a long decimal integer from a string while ensuring that only the
 * decimal value and white space are present in the string.
 * \param s is the string to parse.
 * \param val is dereferenced and set to the extracted value.
 * \return true if val was dereferenced and set; false if the extraction
 * failed
 */
static bool get_long_from_string(const char *s, long *val)
{
	char *endptr;
	long lval = strtol(s, &endptr, 10);

	/*
	 * Reject LONG_MIN and LONG_MAX so it isn't necessary to check errno
	 * to see if the value in the string was out of range.
	 */
	if (s[0] == '\0' ||
			(*endptr != '\0' && !contains_only_spaces(endptr)) ||
			lval <= LONG_MIN || lval >= LONG_MAX) {
		return false;
	}
	*val = lval;
	return true;
}


/**
 * Create an instance of an artifact.
 *
 * \param art The artifact to instantiate.
 * \return An object that represents the artifact.
 */
static struct object *wiz_create_object_from_artifact(const struct artifact *art)
{
	struct object_kind *kind;
	struct object *obj;

	/* Ignore "empty" artifacts */
	if (!art->name) return NULL;

	/* Acquire the "kind" index */
	kind = lookup_kind(art->tval, art->sval);
	if (!kind) return NULL;

	/* Create the artifact */
	obj = object_new();
	object_prep(obj, kind, art->alloc_min, RANDOMISE);
	obj->artifact = art;
	copy_artifact_data(obj, art);

	mark_artifact_created(art, true);

	return obj;
}


/**
 * Create an instance of an object of a given kind.
 *
 * \param kind The base type of object to instantiate.
 * \return An object of the provided object type.
 */
static struct object *wiz_create_object_from_kind(struct object_kind *kind)
{
	struct object *obj;

	if (tval_is_money_k(kind)) {
		obj = make_gold(player->depth, kind->name);
	} else {
		obj = object_new();
		object_prep(obj, kind, player->depth, RANDOMISE);

		/* Apply magic (no messages, no artifacts) */
		apply_magic(obj, player->depth, false, false, false, false);
	}

	return obj;
}


/**
 * Show an item's properties while editing it.  The front end supplies the
 * display; without one, editing works but shows nothing.
 */
void (*wiz_display_item_hook)(const struct object *obj, bool all,
		const struct player *p) = NULL;

static void wiz_display_item(const struct object *obj, bool all,
		const struct player *p)
{
	if (wiz_display_item_hook) wiz_display_item_hook(obj, all, p);
}


/**
 * Drop an object near the player in a manner suitable for debugging.
 *
 * \param obj The object to drop.
 */
static void wiz_drop_object(struct object *obj)
{
	if (obj == NULL) return;

	/* Mark as cheat and where it was created */
	obj->origin = ORIGIN_CHEAT;
	obj->origin_depth = convert_depth_to_origin(player->depth);

	/* Drop the object from heaven. */
	drop_near(cave, &obj, 0, player->grid, true, true);
}


/**
 * Update the queued command object for do_cmd_wiz_play_item() to reflect
 * that the item has changed.
 *
 * Hack:  this assumes that there's only one command queued.
 */
static void wiz_play_item_notify_changed(void)
{
	struct command *cmd = cmdq_peek();

	if (cmd) {
		assert(cmd->code == CMD_WIZ_PLAY_ITEM);
		cmd_set_arg_choice(cmd, "changed", 1);
	}
}


/**
 * Handle the typical updates needed to upkeep flags after playing with an item.
 */
void wiz_play_item_standard_upkeep(struct player *p, struct object *obj)
{
	if (object_is_carried(p, obj)) {
		p->upkeep->update |= (PU_BONUS | PU_INVEN);
		p->upkeep->notice |= (PN_COMBINE);
		p->upkeep->redraw |= (PR_INVEN | PR_EQUIP);
	} else {
		p->upkeep->redraw |= (PR_ITEMLIST);
	}
}


/**
 * Acquire some good or great objects and drop them near the player
 * (CMD_WIZ_ACQUIRE).  Can take the number of objects to acquire from the
 * argument, "quantity", of type number in cmd.  Can take whether to get good
 * or great objects from the argument, "choice", of type choice in cmd:  a
 * non-zero value for that will select great objects.
 */
void do_cmd_wiz_acquire(struct command *cmd)
{
	int n, great;

	if (cmd_get_arg_choice(cmd, "choice", &great) != CMD_OK) {
		great = (get_check("Acquire great objects? ")) ? 1 : 0;
		cmd_set_arg_choice(cmd, "choice", great);
	}

	if (cmd_get_arg_number(cmd, "quantity", &n) != CMD_OK) {
		n = get_quantity((great) ?
			"How many great objects? " : "How many good objects? ",
			40);
		if (n < 1) return;
		cmd_set_arg_number(cmd, "quantity", n);
	}

	acquirement(player->grid, player->depth, n, great);
}


/**
 * Advance the player to level 50 with max stats and other bonuses
 * (CMD_WIZ_ADVANCE).  Takes no arguments from cmd.
 */
void do_cmd_wiz_advance(struct command *cmd)
{
	int i;

	/* Max stats */
	for (i = 0; i < STAT_MAX; i++) {
		player->stat_cur[i] = player->stat_max[i] = 118;
	}

	/* Lots of money */
	player->au = 1000000L;

	/* Level 50 */
	player_exp_gain(player, PY_MAX_EXP);

	/* Heal the player */
	player->chp = player->mhp;
	player->chp_frac = 0;

	/* Restore mana */
	player->csp = player->msp;
	player->csp_frac = 0;

	/* Get some awesome equipment */
	/* Artifacts: 3, 5, 12, ... */

	/* Flag update and redraw for things not handled in player_exp_gain() */
	player->upkeep->redraw |= PR_GOLD | PR_HP | PR_MANA;
}


/**
 * Banish nearby monsters (CMD_WIZ_BANISH).  Can take the range of the effect
 * from the argument, "range", of type number in cmd.
 */
void do_cmd_wiz_banish(struct command *cmd)
{
	int d, i;

	if (cmd_get_arg_number(cmd, "range", &d) != CMD_OK) {
		d = get_quantity("Zap within what distance? ",
			z_info->max_sight);
		cmd_set_arg_number(cmd, "range", d);
	}

	for (i = 1; i < cave_monster_max(cave); i++) {
		struct monster *mon = cave_monster(cave, i);

		/* Skip dead monsters */
		if (!mon->race) continue;

		/* Skip distant monsters */
		if (mon->cdis > d) continue;

		/* Delete the monster */
		delete_monster_idx(cave, i);
	}

	/* Update monster list window */
	player->upkeep->redraw |= PR_MONLIST;
}


/**
 * Change the quantity of an item (CMD_WIZ_CHANGE_ITEM_QUANTITY).  Can take
 * the item to modify from the argument, "item", of type item in cmd.  Can
 * take the new amount from the argument, "quantity", of type number in cmd.
 * Takes whether to update player information from the argument, "update",
 * of type choice in cmd.
 */
void do_cmd_wiz_change_item_quantity(struct command *cmd)
{
	struct object *obj;
	int n, nmax;
	int update = 0;

	if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK) {
		if (!get_item(&obj, "Change quantity of which item? ",
				"You have nothing to change.", cmd->code,
				NULL, (USE_INVEN | USE_QUIVER | USE_FLOOR))) {
			return;
		}
		cmd_set_arg_item(cmd, "item", obj);
	} else if (object_is_equipped(player->body, obj)) {
		msg("Can not change the quantity of an equipped item.");
		return;
	}

	/* Don't allow multiple artifacts. */
	if (obj->artifact) {
		msg("Can not modify the quantity of an artifact.");
		return;
	}

	/* Find limit on stack size. */
	nmax = obj->kind->base->max_stack;
	if (tval_can_have_charges(obj) && obj->pval > 0 && obj->number > 0) {
		/* Items with charges have a limit imposed by MAX_PVAL. */
		nmax = MIN((MAX_PVAL * obj->number) / obj->pval, nmax);
	}
	if (object_is_in_quiver(player, obj)) {
		/* The quiver may have stricter limits. */
		nmax = MIN(z_info->quiver_slot_size /
			(tval_is_ammo(obj) ? 1 : z_info->thrown_quiver_mult),
			nmax);
	}

	/* Get the new quantity. */
	if (cmd_get_arg_number(cmd, "quantity", &n) != CMD_OK) {
		char prompt[80], s[80];

		strnfmt(prompt, sizeof(prompt), "Quantity (1-%d): ", nmax);

		/* Set the default value. */
		strnfmt(s, sizeof(s), "%d", obj->number);

		if (!get_string(prompt, s, sizeof(s)) ||
				!get_int_from_string(s, &n) ||
				n < 1 || n > obj->kind->base->max_stack) {
			return;
		}

		cmd_set_arg_number(cmd, "quantity", n);
	}

	/* Impose limits. */
	n = MAX(1, MIN(nmax, n));

	if (n != obj->number) {
		/* Adjust charges or timeouts for devices. */
		if (tval_can_have_charges(obj) && obj->number > 0) {
			obj->pval = (obj->pval * n) / obj->number;
		}
		if (tval_can_have_timeout(obj) && obj->number > 0) {
			obj->timeout = (obj->timeout * n) / obj->number;
		}

		/* Accept change. */
		if (cmd_get_arg_choice(cmd, "update", &update) != CMD_OK ||
				update) {
			if (object_is_carried(player, obj)) {
				/*
				 * Remove the weight of the old number of
				 * objects.
				 */
				player->upkeep->total_weight -=
					obj->number * object_weight_one(obj);

				/*
				 * Add the weight of the new number of objects.
				 */
				player->upkeep->total_weight +=
					n * object_weight_one(obj);
			}
			wiz_play_item_standard_upkeep(player, obj);
		} else {
			wiz_play_item_notify_changed();
		}
		obj->number = n;
	}
}


/**
 * Generate levels and collect statistics about those with disconnected areas
 * and those where the player is disconnected from stairs
 * (CMD_WIZ_COLLECT_DISCONNECT_STATS).  Can take the number of simulations
 * from the argument, "quantity", of type number in cmd.  Can take whether to
 * stop if a disconnected level is found from the argument, "choice", of type
 * choice in cmd (a nonzero value means stop).
 */
void do_cmd_wiz_collect_disconnect_stats(struct command *cmd)
{
	/* Record last-used value to be the default in next run. */
	static int default_nsim = 50;
	int nsim, stop_on_disconnect;

	if (!stats_are_enabled()) return;

	if (cmd_get_arg_number(cmd, "quantity", &nsim) != CMD_OK) {
		char s[80];

		/* Set default. */
		strnfmt(s, sizeof(s), "%d", default_nsim);

		if (!get_string("Number of simulations: ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &nsim) || nsim < 1) return;
		cmd_set_arg_number(cmd, "quantity", nsim);
	}
	default_nsim = nsim;

	if (cmd_get_arg_choice(cmd, "choice", &stop_on_disconnect) != CMD_OK) {
		stop_on_disconnect =
			get_check("Stop if disconnected level found? ") ? 1 : 0;
		cmd_set_arg_choice(cmd, "choice", stop_on_disconnect);
	}

	disconnect_stats(nsim, stop_on_disconnect != 0);
}


/**
 * Generate levels and collect statistics about the included objects and
 * monsters (CMD_WIZ_COLLECT_OBJ_MON_STATS).  Can take the number of
 * simulations from the argument, "quantity", of type number in cmd.  Can take
 * the type of simulation (diving (1), clearing (2), or clearing with randart
 * regeneration (3)) from the argument, "choice", of type choice in cmd.
 */
void do_cmd_wiz_collect_obj_mon_stats(struct command *cmd)
{
	/* Record last-used values to be the default in next run. */
	static int default_nsim = 50;
	static int default_simtype = 1;
	int nsim, simtype;
	char s[80];

	if (!stats_are_enabled()) return;

	if (cmd_get_arg_number(cmd, "quantity", &nsim) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", default_nsim);

		if (!get_string("Number of simulations: ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &nsim) || nsim < 1) return;
		cmd_set_arg_number(cmd, "quantity", nsim);
	}
	default_nsim = nsim;

	if (cmd_get_arg_choice(cmd, "choice", &simtype) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", default_simtype);

		if (!get_string("Type of Sim: Diving (1) or Clearing (2) ",
			s, sizeof(s))) return;
		if (!get_int_from_string(s, &simtype) || simtype < 1 ||
			simtype > 2) return;
		if (simtype == 2) {
			if (get_check("Regen randarts (warning SLOW)? ")) {
				simtype = 3;
			}
		}
		cmd_set_arg_choice(cmd, "choice", simtype);
	}
	default_simtype = (simtype == 1) ? 1 : 2;

	stats_collect(nsim, simtype);
}


/**
 * Generate several pits and collect statistics about the types of monsters
 * used (CMD_WIZ_COLLECT_PIT_STATS).  Can take the number of simulations from
 * the argument, "quantity", of type number in cmd.  Can take the depth to use
 * for the simulations from the argument, "depth", of type number in cmd.  Can
 * take the type of pit (pit (1), nest (2), or other (3)) from the argument,
 * "choice", of type choice in cmd.
 */
void do_cmd_wiz_collect_pit_stats(struct command *cmd)
{
	int nsim, depth_min, depth_max, pittype;
	char s[80];

	if (!stats_are_enabled()) return;

	if (cmd_get_arg_number(cmd, "quantity", &nsim) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", 1000);

		if (!get_string("Number of simulations per depth: ", s,
				sizeof(s))) return;
		if (!get_int_from_string(s, &nsim) || nsim < 1) return;
		cmd_set_arg_number(cmd, "quantity", nsim);
	}

	if (cmd_get_arg_choice(cmd, "choice", &pittype) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", 1);

		if (!get_string("Pit type (1-3): ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &pittype) || pittype < 1 ||
			pittype > 3) return;
		cmd_set_arg_choice(cmd, "choice", pittype);
	}

	if (cmd_get_arg_number(cmd, "depth_min", &depth_min) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", player->depth);

		if (!get_string("Minimum depth: ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &depth_min)
				|| depth_min < 1) return;
		cmd_set_arg_number(cmd, "depth_min", depth_min);
	}

	if (cmd_get_arg_number(cmd, "depth_max", &depth_max) != CMD_OK) {
		/* Set default. */
		strnfmt(s, sizeof(s), "%d", depth_min);

		if (!get_string("Maximum depth: ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &depth_max)
				|| depth_max < depth_min) return;
		cmd_set_arg_number(cmd, "depth_max", depth_max);
	} else if (depth_max < depth_min) {
		return;
	}

	pit_stats(nsim, pittype, depth_min, depth_max);
}


/**
 * Create all artifacts and drop them near the player
 * (CMD_WIZ_CREATE_ALL_ARTIFACT).  Takes no arguments from cmd.
 */
void do_cmd_wiz_create_all_artifact(struct command *cmd)
{
	int i;

	for (i = 1; i < z_info->a_max; i++) {
		struct artifact *art = &a_info[i];
		struct object *obj = wiz_create_object_from_artifact(art);

		wiz_drop_object(obj);
	}
}


/**
 * Create all artifacts of a given tval and drop them near the player
 * (CMD_WIZ_CREATE_ALL_ARTIFACT_FROM_TVAL).  Can take the tval to use from
 * the argument, "tval", of type number in cmd.
 */
void do_cmd_wiz_create_all_artifact_from_tval(struct command *cmd)
{
	int tval, i;

	if (cmd_get_arg_number(cmd, "tval", &tval) != CMD_OK) {
		char prompt[80];
		char s[80] = "";

		strnfmt(prompt, sizeof(prompt),
			"Create all artifacts of which tval (1-%d)? ",
			TV_MAX - 1);
		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &tval) || tval < 1 ||
			tval >= TV_MAX) return;
		cmd_set_arg_number(cmd, "tval", tval);
	}

	for (i = 1; i < z_info->a_max; i++) {
		struct artifact *art = &a_info[i];

		if (art->tval == tval) {
			struct object *obj =
				wiz_create_object_from_artifact(art);

			wiz_drop_object(obj);
		}
	}
}


/**
 * Create one of each kind of ordinary object and drop them near the player
 * (CMD_WIZ_CREATE_ALL_OBJ).  Takes no arguments from cmd.
 */
void do_cmd_wiz_create_all_obj(struct command *cmd)
{
	int i;

	for (i = 0; i < z_info->k_max; i++) {
		struct object_kind *kind = &k_info[i];
		struct object *obj;

		if (kind->base == NULL || kind->base->name == NULL) continue;
		if (kf_has(kind->kind_flags, KF_INSTA_ART)) continue;

		obj = wiz_create_object_from_kind(kind);
		wiz_drop_object(obj);
	}
}


/**
 * Create one of each kind of object from a tval and drop them near the player
 * (CMD_WIZ_CREATE_ALL_OBJ_FROM_TVAL).  Can take the tval to use from the
 * argument, "tval", of type number in cmd.  Can take whether to create instant
 * artifacts from the argument, "choice", of type choice in cmd.
 */
void do_cmd_wiz_create_all_obj_from_tval(struct command *cmd)
{
	int tval, art;
	int i;

	if (cmd_get_arg_number(cmd, "tval", &tval) != CMD_OK) {
		char prompt[80];
		char s[80] = "";

		strnfmt(prompt, sizeof(prompt),
			"Create all items of which tval (1-%d)? ", TV_MAX - 1);
		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &tval) || tval < 1 ||
			tval >= TV_MAX) return;
		cmd_set_arg_number(cmd, "tval", tval);
	}

	if (cmd_get_arg_choice(cmd, "choice", &art) != CMD_OK) {
		art = get_check("Create instant artifacts? ");
		cmd_set_arg_choice(cmd, "choice", art);
	}

	for (i = 0; i < z_info->k_max; i++) {
		struct object_kind *kind = &k_info[i];
		struct object *obj;

		if (kind->tval != tval ||
			(!art && kf_has(kind->kind_flags, KF_INSTA_ART))) continue;

		obj = wiz_create_object_from_kind(kind);
		wiz_drop_object(obj);
	}
}


/**
 * Create a specified artifact (CMD_WIZ_CREATE_ARTIFACT).  Can take the index
 * of the artifact to create from the argument, "index", of type number in cmd.
 */
void do_cmd_wiz_create_artifact(struct command *cmd)
{
	int ind;

	if (cmd_get_arg_number(cmd, "index", &ind) != CMD_OK) {
		char prompt[80];
		char s[80] = "";

		strnfmt(prompt, sizeof(prompt),
			"Create which artifact (1-%d)? ", z_info->a_max - 1);
		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &ind)) return;
		cmd_set_arg_number(cmd, "index", ind);
	}

	if (ind >= 1 && ind < z_info->a_max) {
		struct artifact *art = &a_info[ind];
		struct object *obj = wiz_create_object_from_artifact(art);

		wiz_drop_object(obj);
	} else {
		msg("That's not a valid artifact.");
	}
}


/**
 * Create an object of a given kind and drop it near the player
 * (CMD_WIZ_CREATE_OBJ).  Can take the index of the kind of object from the
 * argument, "index", of type number in cmd.
 */
void do_cmd_wiz_create_obj(struct command *cmd)
{
	int ind;

	if (cmd_get_arg_number(cmd, "index", &ind) != CMD_OK) {
		char prompt[80];
		char s[80] = "";

		strnfmt(prompt, sizeof(prompt),
			"Create which object (0-%d)? ", z_info->k_max - 1);
		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &ind)) return;
		cmd_set_arg_number(cmd, "index", ind);
	}

	if (ind >= 0 && ind < z_info->k_max) {
		struct object_kind *kind = &k_info[ind];
		struct object *obj = wiz_create_object_from_kind(kind);

		wiz_drop_object(obj);
	} else {
		msg("That's not a valid kind of object.");
	}
}


/**
 * Create a trap at the player's position (CMD_WIZ_CREATE_TRAP).  Can take
 * the type of trap to generate from the argument, "index", of type number
 * in cmd.
 */
void do_cmd_wiz_create_trap(struct command *cmd)
{
	int tidx;

	if (cmd_get_arg_number(cmd, "index", &tidx) != CMD_OK) {
		char s[80] = "";

		if (!get_string("Create which trap? ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &tidx)) {
			const struct trap_kind *trap = lookup_trap(s);

			tidx = (trap) ? trap->tidx : z_info->trap_max;
		}
		cmd_set_arg_number(cmd, "index", tidx);
	}

	if (!square_isfloor(cave, player->grid)
			|| square_isplayertrap(cave, player->grid)
			|| square_iswebbed(cave, player->grid)
			|| square_object(cave, player->grid)) {
		msg("You can't place a trap there!");
	} else if (player->depth == 0) {
		msg("You can't place a trap in the town!");
	} else if (tidx < 1 || tidx >= z_info->trap_max) {
		msg("Trap not found.");
	} else {
		place_trap(cave, player->grid, tidx, 0);
		/* Can not repeat since there's now a trap here. */
		cmd_disable_repeat();
	}
}


/**
 * Instantly cure the player of everything (CMD_WIZ_CURE_ALL).  Takes no
 * arguments from cmd.
 */
void do_cmd_wiz_cure_all(struct command *cmd)
{
	int i;

	/* Remove curses */
	for (i = 0; i < player->body.count; i++) {
		if (player->body.slots[i].obj &&
				player->body.slots[i].obj->curses) {
			mem_free(player->body.slots[i].obj->curses);
			player->body.slots[i].obj->curses = NULL;
		}
	}

	/* Restore stats */
	for (i = 0; i < STAT_MAX; i++) {
		effect_simple(EF_RESTORE_STAT, source_player(), "0", i,
			0, 0, 0, 0, NULL);
	}

	/* Restore the level */
	effect_simple(EF_RESTORE_EXP, source_none(), "0", 0, 0, 0, 0, 0, NULL);

	/* Heal the player */
	player->chp = player->mhp;
	player->chp_frac = 0;

	/* Restore mana */
	player->csp = player->msp;
	player->csp_frac = 0;

	/* Cure stuff */
	(void) player_clear_timed(player, TMD_BLIND, true, false);
	(void) player_clear_timed(player, TMD_CONFUSED, true, false);
	(void) player_clear_timed(player, TMD_POISONED, true, false);
	(void) player_clear_timed(player, TMD_AFRAID, true, false);
	(void) player_clear_timed(player, TMD_PARALYZED, true, false);
	(void) player_clear_timed(player, TMD_IMAGE, true, false);
	(void) player_clear_timed(player, TMD_STUN, true, false);
	(void) player_clear_timed(player, TMD_CUT, true, false);
	(void) player_clear_timed(player, TMD_SLOW, true, false);
	(void) player_clear_timed(player, TMD_AMNESIA, true, false);

	/* No longer hungry */
	player_set_timed(player, TMD_FOOD, PY_FOOD_FULL - 1, false, false);

	/* Flag what needs to be updated or redrawn */
	player->upkeep->update |= PU_TORCH | PU_UPDATE_VIEW | PU_MONSTERS;
	player->upkeep->redraw |= PR_BASIC | PR_EXTRA | PR_MAP | PR_EQUIP;

	/* Give the player some feedback */
	msg("You feel *much* better!");
}


/**
 * Change a curse on an item (CMD_WIZ_CURSE_ITEM).  Can take the item to use
 * from the argument, "item", of type item.  Can take the index of the curse
 * to use from the argument, "index", of type number.  Can take the power of
 * the curse from the argument, "power", of type number.  Using a power of
 * zero will remove a curse.  Takes whether to update player information
 * from the argument, "update", of type choice; when update is zero, something
 * else, presumably do_cmd_wiz_play_item(), handles the update.
 */
void do_cmd_wiz_curse_item(struct command *cmd)
{
	struct object *obj;
	int curse_index, power;
	char s[80];
	int update;

	if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK) {
		if (!get_item(&obj, "Change curse on which item? ",
				"You have nothing to change.", cmd->code,
				NULL, (USE_EQUIP | USE_INVEN | USE_QUIVER |
				USE_FLOOR))) {
			return;
		}
		cmd_set_arg_item(cmd, "item", obj);
	}

	/* Get curse. */
	if (cmd_get_arg_number(cmd, "index", &curse_index) != CMD_OK) {
		strnfmt(s, sizeof(s), "0");
		if (!get_string("Enter curse name or index: ",
				s, sizeof(s))) return;
		if (!get_int_from_string(s, &curse_index)) {
			curse_index = lookup_curse(s);
		}
		cmd_set_arg_number(cmd, "index", curse_index);
	}
	if (curse_index <= 0 || curse_index >= z_info->curse_max) {
		return;
	}

	/* Get power for curse. */
	if (cmd_get_arg_number(cmd, "power", &power) != CMD_OK) {
		strnfmt(s, sizeof(s), "0");
		if (!get_string("Enter curse power (0 removes): ", s,
				sizeof(s)) || !get_int_from_string(s, &power)) {
			return;
		}
		cmd_set_arg_number(cmd, "power", power);
	}
	if (power < 0) {
		return;
	}

	/* Apply. */
	if (power) {
		append_object_curse(obj, curse_index, power);
	} else if (!remove_object_curse(obj, curse_index, false)) {
		return;
	}

	update = 0;
	if (cmd_get_arg_choice(cmd, "update", &update) != CMD_OK || update) {
		wiz_play_item_standard_upkeep(player, obj);
	} else {
		wiz_play_item_notify_changed();
	}
}


/**
 * Detect everything nearby (CMD_WIZ_DETECT_ALL_LOCAL).  Takes no arguments
 * from cmd.
 */
void do_cmd_wiz_detect_all_local(struct command *cmd)
{
	effect_simple(EF_DETECT_TRAPS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_DOORS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_STAIRS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_GOLD, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_OBJECTS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_VISIBLE_MONSTERS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
	effect_simple(EF_DETECT_INVISIBLE_MONSTERS, source_player(), "0",
		0, 0, 0, 22, 40, NULL);
}


/**
 * Detect all monsters (CMD_WIZ_DETECT_ALL_MONSTERS).  Takes no arguments
 * from cmd.
 */
void do_cmd_wiz_detect_all_monsters(struct command *cmd)
{
	effect_simple(EF_DETECT_VISIBLE_MONSTERS, source_player(), "0",
		0, 0, 0, 500, 500, NULL);
	effect_simple(EF_DETECT_INVISIBLE_MONSTERS, source_player(), "0",
		0, 0, 0, 500, 500, NULL);
}


/**
 * Edit the player's amount of experience (CMD_WIZ_EDIT_PLAYER_EXP).  Takes
 * no arguments from cmd.
 */
void do_cmd_wiz_edit_player_exp(struct command *cmd)
{
	char s[80];
	long newv;

	if (edit_player_state == EDIT_PLAYER_BREAK) return;

	/* Set default value. */
	strnfmt(s, sizeof(s), "%ld", (long)(player->exp));

	if (!get_string("Experience: ", s, sizeof(s)) ||
			!get_long_from_string(s, &newv)) {
		/* Set next editing stage to break. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}

	/* Keep in the bounds of [0, PY_MAX_EXP]. */
	newv = MIN(PY_MAX_EXP, MAX(0, newv));

	if (newv > player->exp) {
		player_exp_gain(player, newv - player->exp);
	} else {
		player_exp_lose(player, player->exp - newv, false);
	}
}


/**
 * Edit the player's amount of gold (CMD_WIZ_EDIT_PLAYER_GOLD).  Takes no
 * arguments from cmd.
 */
void do_cmd_wiz_edit_player_gold(struct command *cmd)
{
	char s[80];
	long newv;

	if (edit_player_state == EDIT_PLAYER_BREAK) return;

	/* Set default value. */
	strnfmt(s, sizeof(s), "%ld", (long)(player->au));

	if (!get_string("Gold: ", s, sizeof(s)) ||
			!get_long_from_string(s, &newv)) {
		/* Set next editing stage to break. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}

	/*
	 * Keep in the bounds of [0, maximum int32_t].  Assumes a two's
	 * complement representation.
	 */
	player->au = MIN((int32_t)((1UL << 31) - 1), MAX(0, newv));

	/* Flag what needs to be updated or redrawn. */
	player->upkeep->redraw |= PR_GOLD;
}


/**
 * Start editing the player (CMD_WIZ_EDIT_PLAYER_START).  Takes no arguments
 * from cmd.  Because of the use of static state (edit_player_state), this is
 * not reentrant.
 */
void do_cmd_wiz_edit_player_start(struct command *cmd)
{
	int i;

	if (edit_player_state != EDIT_PLAYER_UNKNOWN) {
		/*
		 * Invoked as the cleanup stage for an edit session to work
		 * nicely with command repetition.
		 */
		edit_player_state = EDIT_PLAYER_UNKNOWN;
		return;
	}
	edit_player_state = EDIT_PLAYER_STARTED;
	for (i = 0; i < STAT_MAX; ++i) {
		if (cmdq_push(CMD_WIZ_EDIT_PLAYER_STAT) != 0) {
			/* Failed.  Skip any queued edit commands. */
			edit_player_state = EDIT_PLAYER_BREAK;
			return;
		}
		cmd_set_arg_choice(cmdq_peek(), "choice", i);
	}
	if (cmdq_push(CMD_WIZ_EDIT_PLAYER_GOLD) != 0) {
		/* Failed.  Skip any queued edit commands. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}
	if (cmdq_push(CMD_WIZ_EDIT_PLAYER_EXP) != 0) {
		/* Failed.  Skip any queued edit commands. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}
	/* Make the last command look like the first so repetition works. */
	if (cmdq_push(CMD_WIZ_EDIT_PLAYER_START) != 0) {
		/* Failed. Skip any queued edit commands. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}
}


/**
 * Edit one of the player's statistics (CMD_WIZ_EDIT_PLAYER_STAT).  Takes
 * the index of the statistic to edit from the argument, "choice", of type
 * choice in the command.
 */
void do_cmd_wiz_edit_player_stat(struct command *cmd)
{
	int stat, newv;
	char prompt[80], s[80];

	if (edit_player_state == EDIT_PLAYER_BREAK) return;

	if (cmd_get_arg_choice(cmd, "choice", &stat) != CMD_OK) {
		strnfmt(prompt, sizeof(prompt),
			"Edit which stat (name or 0-%d): ", STAT_MAX - 1);

		/* Set default value. */
		strnfmt(s, sizeof(s), "%s", stat_idx_to_name(0));

		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &stat)) {
			stat = stat_name_to_idx(s);
			if (stat < 0) {
				return;
			}
		}

		cmd_set_arg_choice(cmd, "choice", stat);
	}

	if (stat < 0 || stat >= STAT_MAX) {
		return;
	}

	strnfmt(prompt, sizeof(prompt), "%s (3-118): ", stat_idx_to_name(stat));

	/* Set default value. */
	strnfmt(s, sizeof(s), "%d", player->stat_max[stat]);

	if (!get_string(prompt, s, sizeof(s)) ||
			!get_int_from_string(s, &newv)) {
		/* Set next editing stage to break. */
		edit_player_state = EDIT_PLAYER_BREAK;
		return;
	}

	/* Limit to the range of [3, 118]. */
	newv = MIN(118, MAX(3, newv));

	player->stat_cur[stat] = player->stat_max[stat] = newv;

	/* Flag what needs to be updated or redrawn. */
	player->upkeep->update |= (PU_BONUS);
	player->upkeep->redraw |= (PR_STATS);
}


/**
 * Hit all monsters in the player's line of sight (CMD_WIZ_HIT_ALL_LOS).  Takes
 * no arguments from cmd.
 */
void do_cmd_wiz_hit_all_los(struct command *cmd)
{
	effect_simple(EF_PROJECT_LOS, source_player(), "10000", PROJ_DISP_ALL,
		0, 0, 0, 0, NULL);
}


/**
 * Increase the player's experience by a given amount (CMD_WIZ_INCREASE_EXP).
 * Can take the amount from the argument, "quantity", of type number in cmd.
 */
void do_cmd_wiz_increase_exp(struct command *cmd)
{
	int n;

	if (cmd_get_arg_number(cmd, "quantity", &n) != CMD_OK) {
		n = get_quantity("Gain how much experience? ", 9999);
		cmd_set_arg_number(cmd, "quantity", n);
	}

	if (n < 1) n = 1;
	player_exp_gain(player, n);
}


/**
 * Go to any level, optionally choosing the level generation algorithm
 * (CMD_WIZ_JUMP_LEVEL).  Can take the level to jump to from the argument,
 * "level", of type number in cmd.  Can take whether to choose the generation
 * algorithm from the argument, "choice", of type choice in cmd.
 *
 * Bugs:
 * Because choose_profile() prompts for the generation algorithm, it is not
 * stored in the command and the prompt will be repeated if the command is
 * repeated.
 */
void do_cmd_wiz_jump_level(struct command *cmd)
{
	int level, choose_gen;

	if (cmd_get_arg_number(cmd, "level", &level) != CMD_OK) {
		char prompt[80], s[80];

		strnfmt(prompt, sizeof(prompt), "Jump to level (0-%d): ",
			z_info->max_depth - 1);

		/* Set default */
		strnfmt(s, sizeof(s), "%d", player->depth);

		if (!get_string(prompt, s, sizeof(s))) return;
		if (!get_int_from_string(s, &level)) return;
		cmd_set_arg_number(cmd, "level", level);
	}

	/* Paranoia */
	if (level < 0 || level >= z_info->max_depth) return;

	if (cmd_get_arg_choice(cmd, "choice", &choose_gen) != CMD_OK) {
		choose_gen = (get_check("Choose cave profile? ")) ? 1 : 0;
		cmd_set_arg_choice(cmd, "choice", choose_gen);
	}

	if (choose_gen) {
		player->noscore |= NOSCORE_JUMPING;
	}

	msg("You jump to dungeon level %d.", level);
	dungeon_change_level(player, level);

	/*
	 * Because of the structure of the game loop, need to take some energy,
	 * or the change level request will not be processed until after
	 * performing another action that takes energy.
	 */
	player->upkeep->energy_use = z_info->move_energy;
}


/**
 * Make the player aware of all object kinds up to a given level
 * (CMD_WIZ_LEARN_OBJECT_KINDS).  Can take the level from the argument,
 * "level", of type number in cmd.
 */
void do_cmd_wiz_learn_object_kinds(struct command *cmd)
{
	int level, i;

	if (cmd_get_arg_number(cmd, "level", &level) != CMD_OK) {
		char s[80] = "100";

		if (!get_string("Learn object kinds up to level (0-100)? ",
			s, sizeof(s))) return;
		if (!get_int_from_string(s, &level)) return;
		cmd_set_arg_number(cmd, "level", level);
	}

	/* Scan all object kinds */
	for (i = 1; i < z_info->k_max; i++) {
		struct object_kind *kind = &k_info[i];

		/* Induce awareness */
		if (kind && kind->name && kind->level <= level) {
			kind->aware = true;
		}
	}

	update_player_object_knowledge(player);
	msg("You now know about many items!");
}


/**
 * Map the area near the player (CMD_WIZ_MAGIC_MAP).  Takes no arguments from
 * cmd.
 */
void do_cmd_wiz_magic_map(struct command *cmd)
{
	effect_simple(EF_MAP_AREA, source_player(), "0", 0, 0, 0, 22, 40, NULL);
}


/**
 * Push objects from a selected grid (CMD_WIZ_PUSH_OBJECT).  Can take the
 * location from the argument, "point", of type point in cmd.
 */
void do_cmd_wiz_push_object(struct command *cmd)
{
	struct loc grid;

	if (cmd_get_arg_point(cmd, "point", &grid) != CMD_OK) {
		if (!get_target_grid(TARGET_KILL, &grid)) return;
		cmd_set_arg_point(cmd, "point", grid);
	}
	push_object(grid);
}


/**
 * Quit without saving (CMD_WIZ_QUIT_NO_SAVE).  Takes no arguments from cmd.
 */
void do_cmd_wiz_quit_no_save(struct command *cmd)
{
	quit("user choice");
}


/**
 * Make the player fully aware of a monster race's attributes
 * (CMD_WIZ_RECALL_MONSTER).  Can take the race from the argument, "index", of
 * type number in cmd.  If that index is -1, make the player aware of all
 * races.
 */
void do_cmd_wiz_recall_monster(struct command *cmd)
{
	int r_idx = z_info->r_max;

	if (cmd_get_arg_number(cmd, "index", &r_idx) != CMD_OK) {
		char s[80] = "";
		char c;

		if (!get_com("Full recall for [a]ll monsters or [s]pecific monster? ", &c)) return;
		if (c == 'a' || c == 'A') {
			r_idx = -1;
		} else if (c == 's' || c == 'S') {
			if (!get_string("Which monster? ", s, sizeof(s)))
				return;
			if (!get_int_from_string(s, &r_idx)) {
				const struct monster_race *race =
					lookup_monster(s);

				if (race) {
					r_idx = race->ridx;
				}
			}
		} else {
			return;
		}
		cmd_set_arg_number(cmd, "index", r_idx);
	}

	if (r_idx >= 0 && r_idx < z_info->r_max) {
		const struct monster_race *race = &r_info[r_idx];

		cheat_monster_lore(race, get_lore(race));
	} else if (r_idx == -1) {
		int i;

		for (i = 0; i < z_info->r_max; i++) {
			cheat_monster_lore(&r_info[i], &l_list[i]);
		}
	} else {
		msg("No monster found.");
	}
}


/**
 * Rerate the player's hit points (CMD_WIZ_RERATE).  Takes no arguments from
 * cmd.
 */
void do_cmd_wiz_rerate(struct command *cmd)
{
	int min_value, max_value, percent;

	min_value = (PY_MAX_LEVEL * 3 * (player->hitdie - 1)) / 8;
	min_value += PY_MAX_LEVEL;

	max_value = (PY_MAX_LEVEL * 5 * (player->hitdie - 1)) / 8;
	max_value += PY_MAX_LEVEL;

	player->player_hp[0] = player->hitdie;

	/* Rerate */
	while (1) {
		int i;

		/* Collect values */
		for (i = 1; i < PY_MAX_LEVEL; i++) {
			player->player_hp[i] = randint1(player->hitdie);
			player->player_hp[i] += player->player_hp[i - 1];
		}

		/* Legal values */
		if (player->player_hp[PY_MAX_LEVEL - 1] >= min_value &&
			player->player_hp[PY_MAX_LEVEL - 1] <= max_value) break;
	}

	percent = (int)(((long)player->player_hp[PY_MAX_LEVEL - 1] * 200L) /
		(player->hitdie + ((PY_MAX_LEVEL - 1) * player->hitdie)));

	/* Update and redraw hitpoints */
	player->upkeep->update |= PU_HP;
	player->upkeep->redraw |= PR_HP;

	msg("Current Life Rating is %d/100.", percent);
}


/**
 * Reroll an item (CMD_WIZ_REROLL_ITEM).  Can take the item to use from
 * the argument, "item", of type item in cmd.  Can take the type of roll
 * to use from the argument, "choice", of type choice in cmd:  0 means set
 * good and great to false for the roll, 1 means set good to true and great
 * to false for the roll, and 2 means set good and great to true for the roll.
 */
void do_cmd_wiz_reroll_item(struct command *cmd)
{
	bool good = false;
	bool great = false;
	int roll_choice = 0;
	int update = 0;
	struct object *obj;
	struct object *new;

	/* Get the item to reroll. */
	if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK) {
		if (!get_item(&obj, "Reroll which item? ",
				"You have nothing to reroll.", cmd->code,
				NULL, (USE_EQUIP | USE_INVEN | USE_QUIVER |
				USE_FLOOR))) {
			return;
		}
		cmd_set_arg_item(cmd, "item", obj);
	}

	/* Get the type of roll to use. */
	if (cmd_get_arg_choice(cmd, "choice", &roll_choice) != CMD_OK) {
		char ch;

		if (!get_com("Roll as [n]ormal, [g]ood, or [e]xcellent? ", &ch)) {
			return;
		}
		if (ch == 'n' || ch == 'N') {
			roll_choice = 0;
		} else if (ch == 'g' || ch == 'G') {
			roll_choice = 1;
		} else if (ch == 'e' || ch == 'E') {
			roll_choice = 2;
		} else {
			return;
		}
		cmd_set_arg_choice(cmd, "choice", roll_choice);
	}
	if (roll_choice == 0) {
		good = false;
		great = false;
	} else if (roll_choice == 1) {
		good = true;
		great = false;
	} else if (roll_choice == 2) {
		good = true;
		great = true;
	} else {
		return;
	}

	/* Leave artifacts alone */
	if (obj->artifact) {
		return;
	}

	/* Get a new object with a clean slate. */
	new = object_new();

	/* Reroll based on old kind and player's depth.  Then apply magic. */
	object_prep(new, obj->kind, player->depth, RANDOMISE);
	apply_magic(new, player->depth, false, good, great, false);

	/* Copy over changes to the original. */
	{
		/* Record old pile information. */
		struct object *prev = obj->prev;
		struct object *next = obj->next;
		struct object *known_obj = obj->known;
		uint16_t oidx = obj->oidx;
		struct loc grid = obj->grid;
		bitflag notice = obj->notice;

		/* Free slays, brands, and curses on the old object by hand. */
		mem_free(obj->slays);
		obj->slays = NULL;
		mem_free(obj->brands);
		obj->brands = NULL;
		mem_free(obj->curses);
		obj->curses = NULL;

		/* Copy over; pile information needs to be restored. */
		object_copy(obj, new);
		obj->prev = prev;
		obj->next = next;
		obj->known = known_obj;
		obj->known->ego = obj->ego;
		obj->oidx = oidx;
		obj->grid = grid;
		obj->notice = notice;
	}

	/* Mark as cheat */
	obj->origin = ORIGIN_CHEAT;

	/* Flag what needs to be updated. */
	if (cmd_get_arg_choice(cmd, "update", &update) != CMD_OK || update) {
		wiz_play_item_standard_upkeep(player, obj);
	} else {
		wiz_play_item_notify_changed();
	}

	/* Free the copy. */
	object_delete(cave, player->cave, &new);
}


/**
 * Summon a specific monster (CMD_WIZ_SUMMON_NAMED).  Can take the index
 * of the monster to summon from the argument, "index", of type number in cmd.
 */
void do_cmd_wiz_summon_named(struct command *cmd)
{
	int r_idx, i = 0;
	struct monster_race *r = NULL;
	struct monster_group_info info = { 0, 0 };

	if (cmd_get_arg_number(cmd, "index", &r_idx) == CMD_OK) {
		if (r_idx > 0 && r_idx < z_info->r_max) {
			r = &r_info[r_idx];
		}
	} else {
		char s[80] = "";

		if (!get_string("Summon which monster? ", s, sizeof(s))) return;
		/* See if an index was entered */
		if (get_int_from_string(s, &r_idx)) {
			if (r_idx > 0 && r_idx < z_info->r_max) {
				r = &r_info[r_idx];
			}
		} else {
			/* If not, find by name */
			r = lookup_monster(s);
		}
		if (r != NULL) {
			cmd_set_arg_number(cmd, "index", r->ridx);
		}
	}

	if (r == NULL) {
		msg("No monster found.");
		return;
	}

	/* Try 10 times */
	while (1) {
		struct loc grid;

		/* Pick an empty location. */
		if (i >= 10 || scatter_ext(cave, &grid, 1, player->grid, 1,
				true, square_isempty) == 0) {
			msg("Could not place monster.");
			break;
		}

		/* Try to place. */
		if (place_new_monster(cave, grid, r, true, true,
				info, ORIGIN_DROP_WIZARD)) {
			player->upkeep->redraw |= PR_MAP | PR_MONLIST;
			break;
		}

		++i;
	}
}


/**
 * Summon random monsters near the player (CMD_WIZ_SUMMON_RANDOM).  Can take
 * the number to summon from the argument, "quantity", of type number in cmd.
 */
void do_cmd_wiz_summon_random(struct command *cmd)
{
	int n, i;

	if (cmd_get_arg_number(cmd, "quantity", &n) != CMD_OK) {
		n = get_quantity("How many monsters? ", 40);
		if (n < 1) n = 1;
		cmd_set_arg_number(cmd, "quantity", n);
	}

	for (i = 0; i < n; i++) {
		effect_simple(EF_SUMMON, source_player(), "1",
			0, 0, 0, 0, 0, NULL);
	}
}


/**
 * Teleport the player randomly with a given approximate range
 * (CMD_WIZ_TELEPORT_RANDOM).  Can take the range from the argument, "range",
 * of type number in cmd.
 */
void do_cmd_wiz_teleport_random(struct command *cmd)
{
	int range;
	char sr[30];

	if (cmd_get_arg_number(cmd, "range", &range) != CMD_OK) {
		char s[80] = "100";

		if (!get_string("Teleport range? ", s, sizeof(s))) return;
		if (!get_int_from_string(s, &range) || range < 1) return;
		cmd_set_arg_number(cmd, "range", range);
	}

	strnfmt(sr, sizeof(sr), "%d", range);
	effect_simple(EF_TELEPORT, source_player(), sr, 0, 0, 0, 0, 0, NULL);
}


/**
 * Teleport to the requested position (CMD_WIZ_TELEPORT_TO).  Can take the
 * position from the argument, "point", of type point in cmd.
 */
void do_cmd_wiz_teleport_to(struct command *cmd)
{
	struct loc grid;

	if (cmd_get_arg_point(cmd, "point", &grid) != CMD_OK) {
		/* Use the targeting function. */
		if (!get_target_grid(TARGET_LOOK, &grid)) return;

		/* Record in the command to facilitate repetition. */
		cmd_set_arg_point(cmd, "point", grid);
	}

	/* Test for passable terrain. */
	if (square_ispassable(cave, grid)) {
		/* Teleport to the target */
		effect_simple(EF_TELEPORT_TO, source_player(), "0", 0, 0, 0,
			grid.y, grid.x, NULL);
	} else {
		msg("The square you are aiming for is impassable.");
	}
}


/**
 * Tweak an item:  make it ego or artifact, give values for modifiers, to_a,
 * to_h, or to_d.  Can take the item to modify from the argument, "item", of
 * type item in cmd.  Takes whether to update player information from the
 * argument, "update", of type choice in cmd.
 */
void do_cmd_wiz_tweak_item(struct command *cmd)
{
	static const char *obj_mods[] = {
		#define STAT(a) #a,
		#include "list-stats.h"
		#undef STAT
		#define OBJ_MOD(a) #a,
		#include "list-object-modifiers.h"
		#undef OBJ_MOD
		NULL
	};
	struct object *obj;
	char tmp_val[80];
	int i, val;
	int update = 0;

	/* Get the item to tweak. */
	if (cmd_get_arg_item(cmd, "item", &obj) != CMD_OK) {
		if (!get_item(&obj, "Tweak which item? ",
				"You have nothing to tweak.", cmd->code,
				NULL, (USE_EQUIP | USE_INVEN | USE_QUIVER |
				USE_FLOOR))) {
			return;
		}
		cmd_set_arg_item(cmd, "item", obj);
	}

	/* Leave artifacts alone */
	if (obj->artifact) return;

	/*
	 * Check for whether updating the player information or let
	 * do_cmd_wiz_play_item() handle it.
	 */
	if (cmd_get_arg_choice(cmd, "update", &update) != CMD_OK) {
		update = 1;
	}

	/* Get ego name. */
	if (obj->ego) {
		strnfmt(tmp_val, sizeof(tmp_val), "%s", obj->ego->name);
	} else {
		strnfmt(tmp_val, sizeof(tmp_val), "-1");
	}
	if (!get_string("Enter ego item: ", tmp_val, sizeof(tmp_val))) return;

	/* Accept index or name */
	if (get_int_from_string(tmp_val, &val)) {
		if (val >= 0 && val < z_info->e_max) {
			obj->ego = &e_info[val];
		} else {
			obj->ego = NULL;
		}
	} else {
		obj->ego = lookup_ego_item(tmp_val, obj->tval, obj->sval);
	}
	if (obj->ego) {
		struct ego_item *e = obj->ego;
		struct object *prev = obj->prev;
		struct object *next = obj->next;
		struct object *known = obj->known;
		uint16_t oidx = obj->oidx;
		struct loc grid = obj->grid;
		bitflag notice = obj->notice;

		object_prep(obj, obj->kind, player->depth, RANDOMISE);
		obj->ego = e;
		obj->prev = prev;
		obj->next = next;
		obj->known = known;
		obj->oidx = oidx;
		obj->grid = grid;
		obj->notice = notice;
		ego_apply_magic(obj, player->depth);
	}
	wiz_display_item(obj, true, player);

	/* Get artifact name */
	if (obj->artifact) {
		strnfmt(tmp_val, sizeof(tmp_val), "%s", obj->artifact->name);
	} else {
		strnfmt(tmp_val, sizeof(tmp_val), "0");
	}
	if (!get_string("Enter new artifact: ", tmp_val, sizeof(tmp_val))) {
		if (update) {
			wiz_play_item_standard_upkeep(player, obj);
		} else {
			wiz_play_item_notify_changed();
		}
		return;
	}

	/* Accept index or name */
	if (get_int_from_string(tmp_val, &val)) {
		if (val > 0 && val < z_info->a_max) {
			obj->artifact = &a_info[val];
		} else {
			obj->artifact = NULL;
		}
	} else {
		obj->artifact = lookup_artifact_name(tmp_val);
	}
	if (obj->artifact) {
		const struct artifact *a = obj->artifact;
		struct object *prev = obj->prev;
		struct object *next = obj->next;
		struct object *known = obj->known;
		uint16_t oidx = obj->oidx;
		struct loc grid = obj->grid;
		bitflag notice = obj->notice;

		obj->ego = NULL;
		object_prep(obj, obj->kind, obj->artifact->alloc_min, RANDOMISE);
		obj->artifact = a;
		obj->prev = prev;
		obj->next = next;
		obj->known = known;
		obj->oidx = oidx;
		obj->grid = grid;
		obj->notice = notice;
		copy_artifact_data(obj, obj->artifact);
	}
	wiz_display_item(obj, true, player);

#define WIZ_TWEAK(attribute, name) do {\
		char prompt[80];\
		strnfmt(prompt, sizeof(prompt), "Enter new %s setting: ", name);\
		strnfmt(tmp_val, sizeof(tmp_val), "%d", obj->attribute);\
		if (!get_string(prompt, tmp_val, sizeof(tmp_val))) {\
			if (update) {\
				wiz_play_item_standard_upkeep(player, obj);\
			} else {\
				wiz_play_item_notify_changed();\
			}\
			return;\
		}\
		if (get_int_from_string(tmp_val, &val)) {\
			obj->attribute = val;\
			wiz_display_item(obj, true, player);\
		}\
} while (0)
	for (i = 0; i < OBJ_MOD_MAX; i++) {
		WIZ_TWEAK(modifiers[i], obj_mods[i]);
	}
	WIZ_TWEAK(to_a, "AC bonus");
	WIZ_TWEAK(to_h, "to-hit");
	WIZ_TWEAK(to_d, "to-dam");

	if (update) {
		wiz_play_item_standard_upkeep(player, obj);
	} else {
		wiz_play_item_notify_changed();
	}
}


/**
 * Make the player ignorant of a monster race's attributes
 * (CMD_WIZ_WIPE_RECALL).  Can take the race from the argument, "index", of
 * type number in cmd.  If that index is -1, make the player ignorant of all
 * races.
 */
void do_cmd_wiz_wipe_recall(struct command *cmd)
{
	int r_idx = z_info->r_max;

	if (cmd_get_arg_number(cmd, "index", &r_idx) != CMD_OK) {
		char s[80] = "";
		char c;

		if (!get_com("Wipe recall for [a]ll monsters or [s]pecific monster? ", &c)) return;
		if (c == 'a' || c == 'A') {
			r_idx = -1;
		} else if (c == 's' || c == 'S') {
			if (!get_string("Which monster? ", s, sizeof(s)))
				return;
			if (!get_int_from_string(s, &r_idx)) {
				const struct monster_race *race =
					lookup_monster(s);

				if (race) {
					r_idx = race->ridx;
				}
			}
		} else {
			return;
		}
		cmd_set_arg_number(cmd, "index", r_idx);
	}

	if (r_idx >= 0 && r_idx < z_info->r_max) {
		const struct monster_race *race = &r_info[r_idx];

		wipe_monster_lore(race, get_lore(race));
	} else if (r_idx == -1) {
		int i;

		for (i = 0; i < z_info->r_max; i++) {
			wipe_monster_lore(&r_info[i], &l_list[i]);
		}
	} else {
		msg("No monster found.");
	}
}


/**
 * Wizard light the level (CMD_WIZ_WIZARD_LIGHT).  Takes no arguments from cmd.
 */
void do_cmd_wiz_wizard_light(struct command *cmd)
{
	wiz_light(cave, player, true);
}
