/**
 * \file net-json.c
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
 */

#include "net-json.h"
#include "z-virt.h"

#include <math.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/**
 * ------------------------------------------------------------------------
 * Writing
 * ------------------------------------------------------------------------ */

void net_jw_init(struct net_jw *w, struct net_buf *b)
{
	memset(w, 0, sizeof(*w));
	w->b = b;
}

static void net_jw_raw(struct net_jw *w, const char *s)
{
	net_buf_append(w->b, s, strlen(s));
}

/**
 * Write the comma that separates this value from the one before it.
 */
static void net_jw_sep(struct net_jw *w)
{
	if (w->after_key) {
		w->after_key = false;
		return;
	}
	if (w->need_comma[w->depth]) net_jw_raw(w, ",");
	w->need_comma[w->depth] = true;
}

static void net_jw_open(struct net_jw *w, const char *bracket)
{
	net_jw_sep(w);
	net_jw_raw(w, bracket);
	if (w->depth < NET_JSON_MAX_DEPTH - 1) w->depth++;
	w->need_comma[w->depth] = false;
}

static void net_jw_close(struct net_jw *w, const char *bracket)
{
	net_jw_raw(w, bracket);
	if (w->depth > 0) w->depth--;
}

void net_jw_obj(struct net_jw *w) { net_jw_open(w, "{"); }
void net_jw_obj_end(struct net_jw *w) { net_jw_close(w, "}"); }
void net_jw_arr(struct net_jw *w) { net_jw_open(w, "["); }
void net_jw_arr_end(struct net_jw *w) { net_jw_close(w, "]"); }

/**
 * Write a quoted string.  Bytes from 0x80 up pass through, since the
 * game's strings are UTF-8.
 */
static void net_jw_quoted(struct net_jw *w, const char *s, size_t n)
{
	size_t i;

	net_jw_raw(w, "\"");
	for (i = 0; i < n; i++) {
		unsigned char c = (unsigned char)s[i];
		char esc[8];

		switch (c) {
			case '"': net_jw_raw(w, "\\\""); break;
			case '\\': net_jw_raw(w, "\\\\"); break;
			case '\n': net_jw_raw(w, "\\n"); break;
			case '\r': net_jw_raw(w, "\\r"); break;
			case '\t': net_jw_raw(w, "\\t"); break;
			default:
				if (c < 0x20) {
					snprintf(esc, sizeof(esc), "\\u%04x", c);
					net_jw_raw(w, esc);
				} else {
					net_buf_append(w->b, &s[i], 1);
				}
		}
	}
	net_jw_raw(w, "\"");
}

void net_jw_key(struct net_jw *w, const char *key)
{
	net_jw_sep(w);
	net_jw_quoted(w, key, strlen(key));
	net_jw_raw(w, ":");
	w->after_key = true;
}

void net_jw_strn(struct net_jw *w, const char *s, size_t n)
{
	net_jw_sep(w);
	net_jw_quoted(w, s, n);
}

void net_jw_str(struct net_jw *w, const char *s)
{
	if (!s) {
		net_jw_null(w);
		return;
	}
	net_jw_strn(w, s, strlen(s));
}

void net_jw_int(struct net_jw *w, long v)
{
	char num[32];

	net_jw_sep(w);
	snprintf(num, sizeof(num), "%ld", v);
	net_jw_raw(w, num);
}

void net_jw_bool(struct net_jw *w, bool v)
{
	net_jw_sep(w);
	net_jw_raw(w, v ? "true" : "false");
}

void net_jw_null(struct net_jw *w)
{
	net_jw_sep(w);
	net_jw_raw(w, "null");
}

void net_jw_kstr(struct net_jw *w, const char *key, const char *s)
{
	net_jw_key(w, key);
	net_jw_str(w, s);
}

void net_jw_kint(struct net_jw *w, const char *key, long v)
{
	net_jw_key(w, key);
	net_jw_int(w, v);
}

void net_jw_kbool(struct net_jw *w, const char *key, bool v)
{
	net_jw_key(w, key);
	net_jw_bool(w, v);
}

/**
 * Write a text block, the game's colored text, as styled text: a list of
 * lines, each a list of spans [{"text": "...", "color": n}, ...] where
 * color is the attribute the game gave that text.  Only newlines split
 * lines; the client wraps them to fit.
 */
void net_jw_textblock(struct net_jw *w, textblock *tb)
{
	const wchar_t *text = textblock_text(tb);
	const uint8_t *attrs = textblock_attrs(tb);
	size_t *starts = NULL, *lengths = NULL;
	size_t n = textblock_calculate_lines(tb, &starts, &lengths, (size_t)-1 / 2);
	struct net_buf span;
	size_t line;

	net_buf_init(&span);
	net_jw_arr(w);
	for (line = 0; line < n; line++) {
		size_t i = starts[line], end = starts[line] + lengths[line];

		net_jw_arr(w);
		while (i < end) {
			uint8_t attr = attrs[i];

			span.len = 0;
			while (i < end && attrs[i] == attr) {
				net_put_utf8(&span, (uint32_t)text[i]);
				i++;
			}
			net_jw_obj(w);
			net_jw_key(w, "text");
			net_jw_strn(w, (const char *)span.data, span.len);
			net_jw_kint(w, "color", attr);
			net_jw_obj_end(w);
		}
		net_jw_arr_end(w);
	}
	net_jw_arr_end(w);
	net_buf_free(&span);
	mem_free(starts);
	mem_free(lengths);
}

/**
 * ------------------------------------------------------------------------
 * Parsing
 * ------------------------------------------------------------------------ */

struct net_jp {
	const char *p;
	const char *end;
	int depth;
};

static void net_jp_space(struct net_jp *jp)
{
	while (jp->p < jp->end && (*jp->p == ' ' || *jp->p == '\t'
			|| *jp->p == '\n' || *jp->p == '\r')) {
		jp->p++;
	}
}

static bool net_jp_lit(struct net_jp *jp, const char *lit)
{
	size_t n = strlen(lit);

	if ((size_t)(jp->end - jp->p) < n || strncmp(jp->p, lit, n)) return false;
	jp->p += n;
	return true;
}

static int net_jp_hex(char c)
{
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
}

/**
 * Parse a quoted string into a new NUL-terminated UTF-8 string.
 */
static char *net_jp_string(struct net_jp *jp)
{
	struct net_buf out;

	if (jp->p >= jp->end || *jp->p != '"') return NULL;
	jp->p++;
	net_buf_init(&out);
	while (jp->p < jp->end && *jp->p != '"') {
		char c = *jp->p++;

		if ((unsigned char)c < 0x20) goto fail;
		if (c != '\\') {
			net_buf_append(&out, &c, 1);
			continue;
		}
		if (jp->p >= jp->end) goto fail;
		c = *jp->p++;
		switch (c) {
			case '"': case '\\': case '/':
				net_buf_append(&out, &c, 1);
				break;
			case 'b': net_buf_append(&out, "\b", 1); break;
			case 'f': net_buf_append(&out, "\f", 1); break;
			case 'n': net_buf_append(&out, "\n", 1); break;
			case 'r': net_buf_append(&out, "\r", 1); break;
			case 't': net_buf_append(&out, "\t", 1); break;
			case 'u': {
				uint32_t cp = 0;
				int i;

				if (jp->end - jp->p < 4) goto fail;
				for (i = 0; i < 4; i++) {
					int h = net_jp_hex(*jp->p++);

					if (h < 0) goto fail;
					cp = cp * 16 + (uint32_t)h;
				}
				/* A surrogate pair makes one code point */
				if (cp >= 0xD800 && cp <= 0xDBFF && jp->end - jp->p >= 6
						&& jp->p[0] == '\\' && jp->p[1] == 'u') {
					uint32_t lo = 0;
					bool ok = true;

					for (i = 2; i < 6; i++) {
						int h = net_jp_hex(jp->p[i]);

						if (h < 0) ok = false;
						lo = lo * 16 + (uint32_t)(h < 0 ? 0 : h);
					}
					if (ok && lo >= 0xDC00 && lo <= 0xDFFF) {
						cp = 0x10000 + ((cp - 0xD800) << 10) + (lo - 0xDC00);
						jp->p += 6;
					}
				}
				net_put_utf8(&out, cp);
				break;
			}
			default:
				goto fail;
		}
	}
	if (jp->p >= jp->end) goto fail;
	jp->p++;
	net_buf_append(&out, "", 1);
	return (char *)out.data;

fail:
	net_buf_free(&out);
	return NULL;
}

static struct net_json *net_jp_value(struct net_jp *jp);

static struct net_json *net_json_new(enum net_json_type type)
{
	struct net_json *v = calloc(1, sizeof(*v));

	if (!v) abort();
	v->type = type;
	return v;
}

/**
 * Parse the members of an object or the elements of an array, after
 * the opening bracket.
 */
static struct net_json *net_jp_container(struct net_jp *jp, bool object)
{
	struct net_json *v = net_json_new(object ? NET_JSON_OBJECT : NET_JSON_ARRAY);
	struct net_json **tail = &v->child;
	char close = object ? '}' : ']';

	if (++jp->depth > NET_JSON_MAX_DEPTH) goto fail;
	net_jp_space(jp);
	if (jp->p < jp->end && *jp->p == close) {
		jp->p++;
		jp->depth--;
		return v;
	}
	while (true) {
		char *key = NULL;
		struct net_json *member;

		net_jp_space(jp);
		if (object) {
			key = net_jp_string(jp);
			if (!key) goto fail;
			net_jp_space(jp);
			if (jp->p >= jp->end || *jp->p != ':') {
				free(key);
				goto fail;
			}
			jp->p++;
		}
		member = net_jp_value(jp);
		if (!member) {
			free(key);
			goto fail;
		}
		member->key = key;
		*tail = member;
		tail = &member->next;

		net_jp_space(jp);
		if (jp->p >= jp->end) goto fail;
		if (*jp->p == ',') {
			jp->p++;
			continue;
		}
		if (*jp->p == close) {
			jp->p++;
			break;
		}
		goto fail;
	}
	jp->depth--;
	return v;

fail:
	net_json_free(v);
	return NULL;
}

static struct net_json *net_jp_number(struct net_jp *jp)
{
	char buf[64];
	size_t n = 0;
	char *endp;
	struct net_json *v;

	while (jp->p + n < jp->end && n < sizeof(buf) - 1
			&& strchr("+-0123456789.eE", jp->p[n])) {
		buf[n] = jp->p[n];
		n++;
	}
	if (!n) return NULL;
	buf[n] = '\0';
	v = net_json_new(NET_JSON_NUMBER);
	v->number = strtod(buf, &endp);
	if (endp != buf + n || !isfinite(v->number)) {
		net_json_free(v);
		return NULL;
	}
	jp->p += n;
	return v;
}

static struct net_json *net_jp_value(struct net_jp *jp)
{
	struct net_json *v;

	net_jp_space(jp);
	if (jp->p >= jp->end) return NULL;
	switch (*jp->p) {
		case '{':
			jp->p++;
			return net_jp_container(jp, true);
		case '[':
			jp->p++;
			return net_jp_container(jp, false);
		case '"': {
			char *s = net_jp_string(jp);

			if (!s) return NULL;
			v = net_json_new(NET_JSON_STRING);
			v->string = s;
			return v;
		}
		case 't':
			if (!net_jp_lit(jp, "true")) return NULL;
			v = net_json_new(NET_JSON_BOOL);
			v->boolean = true;
			return v;
		case 'f':
			if (!net_jp_lit(jp, "false")) return NULL;
			return net_json_new(NET_JSON_BOOL);
		case 'n':
			if (!net_jp_lit(jp, "null")) return NULL;
			return net_json_new(NET_JSON_NULL);
		default:
			return net_jp_number(jp);
	}
}

/**
 * Parse one JSON value that fills text.  Returns NULL if the text is not
 * valid JSON.  Free the result with net_json_free().
 */
struct net_json *net_json_parse(const char *text, size_t len)
{
	struct net_jp jp = { text, text + len, 0 };
	struct net_json *v = net_jp_value(&jp);

	if (!v) return NULL;
	net_jp_space(&jp);
	if (jp.p != jp.end) {
		net_json_free(v);
		return NULL;
	}
	return v;
}

void net_json_free(struct net_json *v)
{
	while (v) {
		struct net_json *next = v->next;

		net_json_free(v->child);
		free(v->string);
		free(v->key);
		free(v);
		v = next;
	}
}

/**
 * Find a member of an object by key, or NULL.
 */
struct net_json *net_json_get(const struct net_json *obj, const char *key)
{
	struct net_json *m;

	if (!obj || obj->type != NET_JSON_OBJECT) return NULL;
	for (m = obj->child; m; m = m->next) {
		if (m->key && !strcmp(m->key, key)) return m;
	}
	return NULL;
}

/**
 * Read a whole number that fits an int.
 */
bool net_json_int(const struct net_json *v, int *out)
{
	if (!v || v->type != NET_JSON_NUMBER) return false;
	if (v->number < -2147483648.0 || v->number > 2147483647.0
			|| (double)(long)v->number != v->number) {
		return false;
	}
	*out = (int)v->number;
	return true;
}

const char *net_json_str(const struct net_json *v)
{
	return (v && v->type == NET_JSON_STRING) ? v->string : NULL;
}

bool net_json_get_int(const struct net_json *obj, const char *key, int *out)
{
	return net_json_int(net_json_get(obj, key), out);
}

const char *net_json_get_str(const struct net_json *obj, const char *key)
{
	return net_json_str(net_json_get(obj, key));
}

bool net_json_get_bool(const struct net_json *obj, const char *key,
		bool *out)
{
	struct net_json *v = net_json_get(obj, key);

	if (!v || v->type != NET_JSON_BOOL) return false;
	*out = v->boolean;
	return true;
}
