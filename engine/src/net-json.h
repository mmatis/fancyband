/**
 * \file net-json.h
 * \brief A small JSON writer and parser for the net front end
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
 * The structured messages (EVENT, SNAPSHOT, REQUEST, COMMAND, REPLY) carry
 * JSON.  This file has no game logic, so unit tests can check it alone.
 */

#ifndef INCLUDED_NET_JSON_H
#define INCLUDED_NET_JSON_H

#include "net-proto.h"

/**
 * How deeply the writer and the parser nest objects and arrays.
 */
#define NET_JSON_MAX_DEPTH 32

/**
 * ------------------------------------------------------------------------
 * Writing
 * ------------------------------------------------------------------------ */

/**
 * Writes JSON into a buffer, adding commas where they belong.
 */
struct net_jw {
	struct net_buf *b;
	int depth;
	bool need_comma[NET_JSON_MAX_DEPTH];
	bool after_key;
};

void net_jw_init(struct net_jw *w, struct net_buf *b);
void net_jw_obj(struct net_jw *w);
void net_jw_obj_end(struct net_jw *w);
void net_jw_arr(struct net_jw *w);
void net_jw_arr_end(struct net_jw *w);
void net_jw_key(struct net_jw *w, const char *key);
void net_jw_str(struct net_jw *w, const char *s);
void net_jw_strn(struct net_jw *w, const char *s, size_t n);
void net_jw_int(struct net_jw *w, long v);
void net_jw_bool(struct net_jw *w, bool v);
void net_jw_null(struct net_jw *w);

/* Key and value together, for the common case */
void net_jw_kstr(struct net_jw *w, const char *key, const char *s);
void net_jw_kint(struct net_jw *w, const char *key, long v);
void net_jw_kbool(struct net_jw *w, const char *key, bool v);

/**
 * ------------------------------------------------------------------------
 * Parsing
 * ------------------------------------------------------------------------ */

enum net_json_type {
	NET_JSON_NULL,
	NET_JSON_BOOL,
	NET_JSON_NUMBER,
	NET_JSON_STRING,
	NET_JSON_ARRAY,
	NET_JSON_OBJECT
};

/**
 * One parsed value.  An object's or array's members are a linked list
 * starting at child; each member of an object has its key set.
 */
struct net_json {
	enum net_json_type type;
	bool boolean;
	double number;
	char *string;
	char *key;
	struct net_json *child;
	struct net_json *next;
};

struct net_json *net_json_parse(const char *text, size_t len);
void net_json_free(struct net_json *v);
struct net_json *net_json_get(const struct net_json *obj, const char *key);
bool net_json_int(const struct net_json *v, int *out);
const char *net_json_str(const struct net_json *v);
bool net_json_get_int(const struct net_json *obj, const char *key, int *out);
const char *net_json_get_str(const struct net_json *obj, const char *key);
bool net_json_get_bool(const struct net_json *obj, const char *key,
		bool *out);

#endif /* INCLUDED_NET_JSON_H */
