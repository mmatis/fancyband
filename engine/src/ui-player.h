/**
 * \file ui-player.h
 * \brief character info
 */

#ifndef UI_PLAYER_H
#define UI_PLAYER_H

void display_player_stat_info(void);
void display_player_xtra_info(void);
void display_player(int mode);
void write_character_dump(ang_file *fff);
bool dump_save(const char *path);
void do_cmd_change_name(void);

typedef void (*player_panel_fn)(int panel, uint8_t attr, const char *label,
	const char *value, void *data);
void player_sheet_panels(player_panel_fn fn, void *data);

/* The columns the labels of display_player_ability_table() take */
#define PLAYER_TABLE_LABEL 6
int display_player_ability_table(void);

#endif /* !UI_PLAYER_H */
