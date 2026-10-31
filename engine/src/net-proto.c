/**
 * \file net-proto.c
 * \brief Frame encoding and decoding for the net front end
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

#include "net-proto.h"

#include <stdlib.h>
#include <string.h>

/**
 * ------------------------------------------------------------------------
 * Buffers
 * ------------------------------------------------------------------------ */

void net_buf_init(struct net_buf *b)
{
	b->data = NULL;
	b->len = 0;
	b->cap = 0;
}

void net_buf_free(struct net_buf *b)
{
	free(b->data);
	net_buf_init(b);
}

/**
 * Drop the first n bytes of the buffer.
 */
void net_buf_consume(struct net_buf *b, size_t n)
{
	if (n >= b->len) {
		b->len = 0;
		return;
	}
	memmove(b->data, b->data + n, b->len - n);
	b->len -= n;
}

/**
 * Make room for n more bytes.  Running out of memory is fatal, as it is
 * everywhere else in the game.
 */
static void net_buf_grow(struct net_buf *b, size_t n)
{
	size_t cap = b->cap ? b->cap : 256;
	uint8_t *data;

	if (b->len + n <= b->cap) return;
	while (cap < b->len + n) cap *= 2;
	data = realloc(b->data, cap);
	if (!data) abort();
	b->data = data;
	b->cap = cap;
}

void net_buf_append(struct net_buf *b, const void *src, size_t n)
{
	if (!n) return;
	net_buf_grow(b, n);
	memcpy(b->data + b->len, src, n);
	b->len += n;
}

/**
 * ------------------------------------------------------------------------
 * Frame building
 * ------------------------------------------------------------------------ */

void net_put_u8(struct net_buf *b, uint8_t v)
{
	net_buf_append(b, &v, 1);
}

void net_put_u16(struct net_buf *b, uint16_t v)
{
	uint8_t bytes[2] = { v & 0xFF, (v >> 8) & 0xFF };
	net_buf_append(b, bytes, sizeof(bytes));
}

void net_put_u32(struct net_buf *b, uint32_t v)
{
	uint8_t bytes[4] = {
		v & 0xFF, (v >> 8) & 0xFF, (v >> 16) & 0xFF, (v >> 24) & 0xFF
	};
	net_buf_append(b, bytes, sizeof(bytes));
}

/**
 * Append one code point as UTF-8.  Surrogates and values past U+10FFFF
 * become U+FFFD, the replacement character.
 */
void net_put_utf8(struct net_buf *b, uint32_t cp)
{
	uint8_t bytes[4];
	size_t n;

	if ((cp >= 0xD800 && cp <= 0xDFFF) || cp > 0x10FFFF) cp = 0xFFFD;

	if (cp < 0x80) {
		bytes[0] = cp;
		n = 1;
	} else if (cp < 0x800) {
		bytes[0] = 0xC0 | (cp >> 6);
		bytes[1] = 0x80 | (cp & 0x3F);
		n = 2;
	} else if (cp < 0x10000) {
		bytes[0] = 0xE0 | (cp >> 12);
		bytes[1] = 0x80 | ((cp >> 6) & 0x3F);
		bytes[2] = 0x80 | (cp & 0x3F);
		n = 3;
	} else {
		bytes[0] = 0xF0 | (cp >> 18);
		bytes[1] = 0x80 | ((cp >> 12) & 0x3F);
		bytes[2] = 0x80 | ((cp >> 6) & 0x3F);
		bytes[3] = 0x80 | (cp & 0x3F);
		n = 4;
	}
	net_buf_append(b, bytes, n);
}

/**
 * Append n wide characters as UTF-8.  The game stores text as wchar_t,
 * which holds UTF-32 on the systems the net front end supports.
 */
void net_put_wide(struct net_buf *b, const wchar_t *s, int n)
{
	int i;

	for (i = 0; i < n; i++) {
		net_put_utf8(b, (uint32_t)s[i]);
	}
}

/**
 * Append a string as a one-byte length and up to 255 bytes.
 */
static void net_put_short_str(struct net_buf *b, const char *s)
{
	size_t n = strlen(s);

	if (n > 255) n = 255;
	net_put_u8(b, (uint8_t)n);
	net_buf_append(b, s, n);
}

/**
 * Start a frame of the given type.  Returns where the frame starts, to
 * pass to net_frame_end() once the payload is written.
 */
size_t net_frame_begin(struct net_buf *b, uint8_t type)
{
	size_t start = b->len;

	net_put_u32(b, 0);
	net_put_u8(b, type);
	return start;
}

/**
 * Fill in the length of the frame that starts at start.
 */
void net_frame_end(struct net_buf *b, size_t start)
{
	uint32_t len = (uint32_t)(b->len - start - 4);

	b->data[start] = len & 0xFF;
	b->data[start + 1] = (len >> 8) & 0xFF;
	b->data[start + 2] = (len >> 16) & 0xFF;
	b->data[start + 3] = (len >> 24) & 0xFF;
}

/**
 * ------------------------------------------------------------------------
 * Messages
 * ------------------------------------------------------------------------ */

void net_msg_hello(struct net_buf *b, const struct net_hello *h)
{
	size_t start = net_frame_begin(b, NET_HELLO);
	int i;

	net_put_u16(b, h->version);
	net_put_short_str(b, h->name);
	net_put_u8(b, (uint8_t)h->n_terms);
	for (i = 0; i < h->n_terms; i++) {
		net_put_u8(b, h->terms[i].index);
		net_put_u16(b, h->terms[i].width);
		net_put_u16(b, h->terms[i].height);
	}
	net_put_u8(b, (uint8_t)h->n_caps);
	for (i = 0; i < h->n_caps; i++) {
		net_put_short_str(b, h->caps[i]);
	}
	net_frame_end(b, start);
}

void net_msg_term_open(struct net_buf *b, int term, int w, int h,
		const char *name)
{
	size_t start = net_frame_begin(b, NET_TERM_OPEN);

	net_put_u8(b, (uint8_t)term);
	net_put_u16(b, (uint16_t)w);
	net_put_u16(b, (uint16_t)h);
	net_buf_append(b, name, strlen(name));
	net_frame_end(b, start);
}

void net_msg_palette(struct net_buf *b, const uint8_t (*rgb)[3], int n)
{
	size_t start = net_frame_begin(b, NET_PALETTE);
	int i;

	net_put_u8(b, (uint8_t)n);
	for (i = 0; i < n; i++) {
		net_buf_append(b, rgb[i], 3);
	}
	net_frame_end(b, start);
}

void net_msg_text(struct net_buf *b, int term, int x, int y, int attr,
		const wchar_t *s, int n)
{
	size_t start = net_frame_begin(b, NET_TEXT);

	net_put_u8(b, (uint8_t)term);
	net_put_u16(b, (uint16_t)x);
	net_put_u16(b, (uint16_t)y);
	net_put_u16(b, (uint16_t)attr);
	net_put_wide(b, s, n);
	net_frame_end(b, start);
}

void net_msg_wipe(struct net_buf *b, int term, int x, int y, int n)
{
	size_t start = net_frame_begin(b, NET_WIPE);

	net_put_u8(b, (uint8_t)term);
	net_put_u16(b, (uint16_t)x);
	net_put_u16(b, (uint16_t)y);
	net_put_u16(b, (uint16_t)n);
	net_frame_end(b, start);
}

void net_msg_clear(struct net_buf *b, int term)
{
	size_t start = net_frame_begin(b, NET_CLEAR);

	net_put_u8(b, (uint8_t)term);
	net_frame_end(b, start);
}

void net_msg_cursor(struct net_buf *b, int term, int x, int y, int flags)
{
	size_t start = net_frame_begin(b, NET_CURSOR);

	net_put_u8(b, (uint8_t)term);
	net_put_u16(b, (uint16_t)x);
	net_put_u16(b, (uint16_t)y);
	net_put_u8(b, (uint8_t)flags);
	net_frame_end(b, start);
}

void net_msg_fresh(struct net_buf *b, int term)
{
	size_t start = net_frame_begin(b, NET_FRESH);

	net_put_u8(b, (uint8_t)term);
	net_frame_end(b, start);
}

void net_msg_bell(struct net_buf *b)
{
	net_frame_end(b, net_frame_begin(b, NET_BELL));
}

void net_msg_bye(struct net_buf *b, const char *reason)
{
	size_t start = net_frame_begin(b, NET_BYE);

	net_buf_append(b, reason, strlen(reason));
	net_frame_end(b, start);
}

void net_msg_tileset(struct net_buf *b, const struct net_tileset *t)
{
	size_t start = net_frame_begin(b, NET_TILESET);

	net_put_u8(b, t->id);
	net_put_u16(b, t->tile_w);
	net_put_u16(b, t->tile_h);
	net_put_u8(b, t->cells_w);
	net_put_u8(b, t->cells_h);
	net_put_u8(b, t->alpha);
	net_put_short_str(b, t->name);
	net_buf_append(b, t->path, strlen(t->path));
	net_frame_end(b, start);
}

void net_msg_pict(struct net_buf *b, int term, int x, int y,
		const struct net_pict *cells, int n)
{
	size_t start = net_frame_begin(b, NET_PICT);
	int i;

	net_put_u8(b, (uint8_t)term);
	net_put_u16(b, (uint16_t)x);
	net_put_u16(b, (uint16_t)y);
	net_put_u16(b, (uint16_t)n);
	for (i = 0; i < n; i++) {
		net_put_u8(b, cells[i].fg_row);
		net_put_u8(b, cells[i].fg_col);
		net_put_u8(b, cells[i].bg_row);
		net_put_u8(b, cells[i].bg_col);
	}
	net_frame_end(b, start);
}

void net_msg_key(struct net_buf *b, const struct net_key *k)
{
	size_t start = net_frame_begin(b, NET_KEY);

	net_put_u32(b, k->code);
	net_put_u8(b, k->mods);
	net_frame_end(b, start);
}

void net_msg_mouse(struct net_buf *b, const struct net_mouse *m)
{
	size_t start = net_frame_begin(b, NET_MOUSE);

	net_put_u8(b, m->term);
	net_put_u16(b, m->x);
	net_put_u16(b, m->y);
	net_put_u8(b, m->button);
	net_put_u8(b, m->mods);
	net_frame_end(b, start);
}

void net_msg_resize(struct net_buf *b, const struct net_resize *r)
{
	size_t start = net_frame_begin(b, NET_RESIZE);

	net_put_u8(b, r->term);
	net_put_u16(b, r->width);
	net_put_u16(b, r->height);
	net_frame_end(b, start);
}

void net_msg_quit(struct net_buf *b)
{
	net_frame_end(b, net_frame_begin(b, NET_QUIT));
}

/**
 * ------------------------------------------------------------------------
 * Decoding
 * ------------------------------------------------------------------------ */

/**
 * Find the first frame in data.  Returns the number of bytes the frame
 * takes, 0 if more bytes are needed, or -1 if the length field is
 * invalid, which means the stream cannot be trusted any further.
 */
int net_frame_next(const uint8_t *data, size_t len, struct net_frame *f)
{
	uint32_t flen;

	if (len < 4) return 0;
	flen = (uint32_t)data[0] | ((uint32_t)data[1] << 8)
		| ((uint32_t)data[2] << 16) | ((uint32_t)data[3] << 24);
	if (flen < 1 || flen > NET_FRAME_MAX) return -1;
	if (len < 4 + (size_t)flen) return 0;

	f->type = data[4];
	f->payload = data + NET_FRAME_HEADER;
	f->len = flen - 1;
	return (int)(4 + flen);
}

void net_reader_init(struct net_reader *r, const struct net_frame *f)
{
	r->data = f->payload;
	r->len = f->len;
	r->pos = 0;
	r->error = false;
}

static bool net_reader_need(struct net_reader *r, size_t n)
{
	if (r->error || r->len - r->pos < n) {
		r->error = true;
		return false;
	}
	return true;
}

uint8_t net_get_u8(struct net_reader *r)
{
	if (!net_reader_need(r, 1)) return 0;
	return r->data[r->pos++];
}

uint16_t net_get_u16(struct net_reader *r)
{
	uint16_t v;

	if (!net_reader_need(r, 2)) return 0;
	v = (uint16_t)(r->data[r->pos] | (r->data[r->pos + 1] << 8));
	r->pos += 2;
	return v;
}

uint32_t net_get_u32(struct net_reader *r)
{
	uint32_t v;

	if (!net_reader_need(r, 4)) return 0;
	v = (uint32_t)r->data[r->pos] | ((uint32_t)r->data[r->pos + 1] << 8)
		| ((uint32_t)r->data[r->pos + 2] << 16)
		| ((uint32_t)r->data[r->pos + 3] << 24);
	r->pos += 4;
	return v;
}

/**
 * Read a one-byte length and that many bytes into dest, cutting the
 * string to fit dest if needed.
 */
static void net_get_short_str(struct net_reader *r, char *dest, size_t size)
{
	size_t n = net_get_u8(r);
	size_t keep = (n < size - 1) ? n : size - 1;

	dest[0] = '\0';
	if (!net_reader_need(r, n)) return;
	memcpy(dest, r->data + r->pos, keep);
	dest[keep] = '\0';
	r->pos += n;
}

/**
 * Parse a HELLO payload.  Terms and capabilities past the limits in
 * net-proto.h are read and dropped.
 */
bool net_parse_hello(const struct net_frame *f, struct net_hello *h)
{
	struct net_reader r;
	char skip[NET_HELLO_MAX_STR];
	int i, n;

	memset(h, 0, sizeof(*h));
	if (f->type != NET_HELLO) return false;
	net_reader_init(&r, f);

	h->version = net_get_u16(&r);
	net_get_short_str(&r, h->name, sizeof(h->name));

	n = net_get_u8(&r);
	for (i = 0; i < n; i++) {
		struct net_term_size t;

		t.index = net_get_u8(&r);
		t.width = net_get_u16(&r);
		t.height = net_get_u16(&r);
		if (h->n_terms < NET_HELLO_MAX_TERMS) h->terms[h->n_terms++] = t;
	}

	n = net_get_u8(&r);
	for (i = 0; i < n; i++) {
		if (h->n_caps < NET_HELLO_MAX_CAPS) {
			net_get_short_str(&r, h->caps[h->n_caps++], NET_HELLO_MAX_STR);
		} else {
			net_get_short_str(&r, skip, sizeof(skip));
			h->dropped_caps++;
		}
	}

	return !r.error;
}

bool net_parse_key(const struct net_frame *f, struct net_key *k)
{
	struct net_reader r;

	if (f->type != NET_KEY) return false;
	net_reader_init(&r, f);
	k->code = net_get_u32(&r);
	k->mods = net_get_u8(&r);
	return !r.error;
}

bool net_parse_mouse(const struct net_frame *f, struct net_mouse *m)
{
	struct net_reader r;

	if (f->type != NET_MOUSE) return false;
	net_reader_init(&r, f);
	m->term = net_get_u8(&r);
	m->x = net_get_u16(&r);
	m->y = net_get_u16(&r);
	m->button = net_get_u8(&r);
	m->mods = net_get_u8(&r);
	return !r.error;
}

bool net_parse_resize(const struct net_frame *f, struct net_resize *rs)
{
	struct net_reader r;

	if (f->type != NET_RESIZE) return false;
	net_reader_init(&r, f);
	rs->term = net_get_u8(&r);
	rs->width = net_get_u16(&r);
	rs->height = net_get_u16(&r);
	return !r.error;
}
