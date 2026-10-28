/**
 * \file ui-spoil.h
 * \brief Declarations for menu to generate spoiler files
 */
#ifndef INCLUDED_UI_SPOIL_H
#define INCLUDED_UI_SPOIL_H

struct command;

/* ui-spoil.c */
void do_cmd_spoilers(void);

/* ui-cmd-spoil.c */
void do_cmd_spoil_artifact(struct command *cmd);
void do_cmd_spoil_monster(struct command *cmd);
void do_cmd_spoil_monster_brief(struct command *cmd);
void do_cmd_spoil_obj(struct command *cmd);
void textui_register_spoiler_commands(void);

/* wiz-spoil.c */
void spoil_artifact(const char *fname);
void spoil_mon_desc(const char *fname);
void spoil_mon_info(const char *fname);
void spoil_obj_desc(const char *fname);

#endif /* INCLUDED_UI_SPOIL_H */
