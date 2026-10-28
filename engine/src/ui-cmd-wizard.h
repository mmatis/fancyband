/**
 * \file ui-cmd-wizard.h
 * \brief Debug commands whose work is mostly display
 *
 * Copyright (c) 2021 Eric Branlund
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

#ifndef INCLUDED_UI_CMD_WIZARD_H
#define INCLUDED_UI_CMD_WIZARD_H

struct command;

void do_cmd_wiz_display_keylog(struct command *cmd);
void do_cmd_wiz_dump_level_map(struct command *cmd);
void do_cmd_wiz_peek_noise_scent(struct command *cmd);
void do_cmd_wiz_perform_effect(struct command *cmd);
void do_cmd_wiz_play_item(struct command *cmd);
void do_cmd_wiz_query_feature(struct command *cmd);
void do_cmd_wiz_query_square_flag(struct command *cmd);
void do_cmd_wiz_stat_item(struct command *cmd);

void textui_register_wizard_commands(void);

#endif /* !INCLUDED_UI_CMD_WIZARD_H */
