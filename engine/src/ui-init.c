/**
 * \file ui-init.c
 * \brief UI initialistion
 *
 * Copyright (c) 2015 Nick McConnell
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
 * This file is used to initialize various variables and arrays for the
 * Angband game.
 *
 * Several of the arrays for Angband are built from data files in the
 * "lib/gamedata" directory.
 */


#include "angband.h"
#include "game-input.h"
#include "game-event.h"
#include "init.h"
#include "ui-display.h"
#include "ui-entry.h"
#include "ui-entry-init.h"
#include "ui-game.h"
#include "ui-init.h"
#include "ui-input.h"
#include "ui-keymap.h"
#include "ui-knowledge.h"
#include "ui-options.h"
#include "ui-output.h"
#include "ui-prefs.h"
#include "ui-term.h"
#include "ui-visuals.h"

extern struct init_module ui_player_module;
extern struct init_module ui_equip_cmp_module;

/**
 * Load the UI entry renderers and entries (ui_entry_renderer.txt and
 * ui_entry.txt).  The engine binds player and object properties to these
 * entries while it parses its own data, so they must load first.
 */
static void ui_entry_module_init(void)
{
	event_signal_message(EVENT_INITSTATUS, 0,
		"Initializing ui renderers...");
	if (run_parser(&ui_entry_renderer_parser)) {
		quit("Cannot initialize ui renderers.");
	}
	event_signal_message(EVENT_INITSTATUS, 0, "Initializing ui entries...");
	if (run_parser(&ui_entry_parser)) {
		quit("Cannot initialize ui entries.");
	}
}

static void ui_entry_module_cleanup(void)
{
	cleanup_parser(&ui_entry_renderer_parser);
	cleanup_parser(&ui_entry_parser);
}

static struct init_module ui_entry_module = {
	.name = "ui-entry",
	.init = ui_entry_module_init,
	.cleanup = ui_entry_module_cleanup
};

/**
 * Plug the text UI into the engine.  Call this before init_angband().  It
 * is safe to call more than once.
 */
void textui_register_with_engine(void)
{
	/* Visuals must load before monsters, which name their color cycles. */
	init_add_module(INIT_STAGE_BEFORE_DATA, &ui_visuals_module);
	init_add_module(INIT_STAGE_BEFORE_DATA, &ui_entry_module);
	init_add_module(INIT_STAGE_AFTER_ENGINE, &ui_player_module);
	init_add_module(INIT_STAGE_AFTER_ENGINE, &ui_equip_cmp_module);

	bind_player_ability_hook = bind_player_ability_to_ui_entry_by_name;
	bind_object_property_hook = bind_object_property_to_ui_entry_by_name;
	set_race_color_cycle_hook = visuals_cycler_set_cycle_for_race;
}

/**
 * Initialise the UI
 */
void textui_init(void)
{
	uint32_t default_window_flag[ANGBAND_TERM_MAX];

	/* Initialize graphics info and basic pref data */
	event_signal_message(EVENT_INITSTATUS, 0, "Loading basic pref file...");
	(void)process_pref_file("pref.prf", false, false);

	if (!play_again) {
		/* Sneakily init command list */
		cmd_init();

		/* Initialize knowledge things */
		textui_knowledge_init();

		/* Initialize input hooks */
		textui_input_init();

		/* Initialize visual prefs */
		textui_prefs_init();

		/* Increase "icky" depth */
		screen_save_depth++;

		/* Verify main term */
		if (!term_screen)
			quit("Main window does not exist");

		/* Make sure main term is active */
		Term_activate(term_screen);

		/* Verify minimum size */
		if ((Term->hgt < 24) || (Term->wid < 80))
			plog("Main window is too small - please make it bigger.");

		/* Turn off the cursor */
		(void)Term_set_cursor(false);

		/* Update terminals for preference changes. */
		(void) Term_xtra(TERM_XTRA_REACT, 0);
		(void) Term_redraw_all();
	} else {
		/* Redo knowledge initialization. */
		textui_knowledge_cleanup();
		textui_knowledge_init();
	}

	/* initialize window options that will be overridden by the savefile */
	memset(window_flag, 0, sizeof(uint32_t)*ANGBAND_TERM_MAX);
	memset(default_window_flag, 0, sizeof default_window_flag);
	if (ANGBAND_TERM_MAX > 1) default_window_flag[1] = (PW_MESSAGE);
	if (ANGBAND_TERM_MAX > 2) default_window_flag[2] = (PW_INVEN);
	if (ANGBAND_TERM_MAX > 3) default_window_flag[3] = (PW_MONLIST);
	if (ANGBAND_TERM_MAX > 4) default_window_flag[4] = (PW_ITEMLIST);
	if (ANGBAND_TERM_MAX > 5) default_window_flag[5] = (PW_MONSTER | PW_OBJECT);
	if (ANGBAND_TERM_MAX > 6) default_window_flag[6] = (PW_OVERHEAD);
	if (ANGBAND_TERM_MAX > 7) default_window_flag[7] = (PW_PLAYER_2);

	/* Set up the subwindows */
	subwindows_set_flags(default_window_flag, ANGBAND_TERM_MAX);

	/* Done */
	event_signal_message(EVENT_INITSTATUS, 0, "Initialization complete");
}


/**
 * Clean up UI
 */
void textui_cleanup(void)
{
	/* Cleanup any options menus */
	cleanup_options();

	keymap_free();
	textui_prefs_free();
	textui_knowledge_cleanup();
}
