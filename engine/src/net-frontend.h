/**
 * \file net-frontend.h
 * \brief What main-net.c and net-game.c share
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
 * main-net.c owns the socket and streams the terms.  net-game.c carries
 * the structured channel: events, snapshots, commands, and prompts sent
 * as requests, and it runs the game in headless mode.
 */

#ifndef INCLUDED_NET_FRONTEND_H
#define INCLUDED_NET_FRONTEND_H

#include "net-proto.h"

/* main-net.c */
extern struct net_buf net_out;
extern bool net_headless;
void net_flush(void);
void net_wait_input(void);
void net_save_and_exit(const char *reason);
void net_exit(const char *reason);
bool net_is_exiting(void);
const char *net_set_tiles(int id, int scale);
bool net_read_tile_sets(void);

/* net-game.c */
extern char net_defaults_dir[1024];
void net_game_hello(const struct net_hello *hello);
void net_game_install(void);
bool net_game_key_wait(void);
void net_game_frame(const struct net_frame *f);
void net_game_play(bool new_game);
void net_game_before_flush(void);

#endif /* INCLUDED_NET_FRONTEND_H */
