/**
 * \file ui-cmd-spoil.c
 * \brief Shims between the Angband 4 command system and spoiler generation
 *
 * Spoiler generation uses the text UI's monster recall, so the text UI
 * supplies these handlers to the engine with cmd_set_frontend_handler().
 *
 * Copyright (c) 2021 Eric Branlund
 *
 * This work is free software; you can redistribute it and/or modify it under
 * the terms of either:
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
#include "ui-spoil.h"


/**
 * Generate artifact spoilers (CMD_SPOIL_ARTIFACT).  Takes no arguments
 * from cmd.
 */
void do_cmd_spoil_artifact(struct command *cmd)
{
	spoil_artifact("artifact.spo");
}


/**
 * Generate monster spoilers (CMD_SPOIL_MON).  Takes no arguments from cmd.
 */
void do_cmd_spoil_monster(struct command *cmd)
{
	spoil_mon_info("mon-info.spo");
}


/**
 * Generate brief monster spoilers (CMD_SPOIL_MON_BRIEF).  Takes no arguments
 * from cmd.
 */
void do_cmd_spoil_monster_brief(struct command *cmd)
{
	spoil_mon_desc("mon-desc.spo");
}


/**
 * Generate object spoilers (CMD_SPOIL_OBJ).  Takes no arguments from cmd.
 */
void do_cmd_spoil_obj(struct command *cmd)
{
	spoil_obj_desc("obj-desc.spo");
}


/**
 * Give the engine the spoiler commands above.  Safe to call more than once.
 */
void textui_register_spoiler_commands(void)
{
	(void) cmd_set_frontend_handler(CMD_SPOIL_ARTIFACT,
		do_cmd_spoil_artifact);
	(void) cmd_set_frontend_handler(CMD_SPOIL_MON, do_cmd_spoil_monster);
	(void) cmd_set_frontend_handler(CMD_SPOIL_MON_BRIEF,
		do_cmd_spoil_monster_brief);
	(void) cmd_set_frontend_handler(CMD_SPOIL_OBJ, do_cmd_spoil_obj);
}
