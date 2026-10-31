/**
 * \file net-proto.h
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
 *
 * This file holds no game logic, so unit tests can check it alone.  The
 * protocol it implements is written up in proto/README.md; the test
 * vectors in proto/vectors.txt are shared with the Go client.
 */

#ifndef INCLUDED_NET_PROTO_H
#define INCLUDED_NET_PROTO_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>
#include <wchar.h>

/**
 * The protocol version this build speaks.
 */
#define NET_PROTO_VERSION 1

/**
 * Bytes in the frame header: a 32-bit length and a type byte.  The length
 * counts the type byte and the payload, so it is at least 1.
 */
#define NET_FRAME_HEADER 5

/**
 * The longest frame either side accepts, counting the type byte.
 */
#define NET_FRAME_MAX (1024 * 1024)

/**
 * Limits on the HELLO message.
 */
#define NET_HELLO_MAX_TERMS 8
#define NET_HELLO_MAX_CAPS 64
#define NET_HELLO_MAX_STR 64

/**
 * Bits in the CURSOR flags byte.
 */
#define NET_CURSOR_VISIBLE 0x01
#define NET_CURSOR_BIG 0x02

enum net_msg_type {
	/* Both directions */
	NET_HELLO = 0x01,

	/* Server to client */
	NET_TERM_OPEN = 0x10,
	NET_PALETTE = 0x11,
	NET_TEXT = 0x12,
	NET_WIPE = 0x13,
	NET_CLEAR = 0x14,
	NET_CURSOR = 0x15,
	NET_FRESH = 0x16,
	NET_BELL = 0x17,
	NET_SOUND = 0x18,
	NET_TILESET = 0x19,
	NET_PICT = 0x1A,
	NET_EVENT = 0x20,
	NET_SNAPSHOT = 0x21,
	NET_REQUEST = 0x22,
	NET_BYE = 0x3F,

	/* Client to server */
	NET_KEY = 0x40,
	NET_MOUSE = 0x41,
	NET_RESIZE = 0x42,
	NET_COMMAND = 0x50,
	NET_REPLY = 0x51,
	NET_QUIT = 0x7F
};

/**
 * A growable byte buffer that frames are written into.
 */
struct net_buf {
	uint8_t *data;
	size_t len;
	size_t cap;
};

/**
 * One decoded frame.  The payload points into the caller's buffer.
 */
struct net_frame {
	uint8_t type;
	const uint8_t *payload;
	size_t len;
};

/**
 * Reads fixed-size fields from a payload.  Reading past the end sets
 * error and returns zeros.
 */
struct net_reader {
	const uint8_t *data;
	size_t len;
	size_t pos;
	bool error;
};

struct net_term_size {
	uint8_t index;
	uint16_t width;
	uint16_t height;
};

/**
 * The HELLO payload, the same in both directions.  The server lists no
 * terms; it announces each one with TERM_OPEN instead.
 */
struct net_hello {
	uint16_t version;
	char name[NET_HELLO_MAX_STR];
	int n_terms;
	struct net_term_size terms[NET_HELLO_MAX_TERMS];
	int n_caps;
	char caps[NET_HELLO_MAX_CAPS][NET_HELLO_MAX_STR];
	/* How many capabilities past NET_HELLO_MAX_CAPS were dropped */
	int dropped_caps;
};

/**
 * The tile set the engine draws the map with; see NET_TILESET.
 */
struct net_tileset {
	uint8_t id;
	uint16_t tile_w;	/* Pixels */
	uint16_t tile_h;
	uint8_t cells_w;	/* Cells each tile covers */
	uint8_t cells_h;
	uint8_t alpha;		/* The sheet has an alpha channel */
	const char *name;
	const char *path;	/* The sheet, as an absolute path */
};

/**
 * One cell of a PICT run: the tile to draw and the terrain tile under
 * it, as rows and columns in the sheet.
 */
struct net_pict {
	uint8_t fg_row, fg_col;
	uint8_t bg_row, bg_col;
};

struct net_key {
	uint32_t code;
	uint8_t mods;
};

struct net_mouse {
	uint8_t term;
	uint16_t x;
	uint16_t y;
	uint8_t button;
	uint8_t mods;
};

struct net_resize {
	uint8_t term;
	uint16_t width;
	uint16_t height;
};

/* Buffers */
void net_buf_init(struct net_buf *b);
void net_buf_free(struct net_buf *b);
void net_buf_consume(struct net_buf *b, size_t n);
void net_buf_append(struct net_buf *b, const void *src, size_t n);

/* Low-level frame building */
size_t net_frame_begin(struct net_buf *b, uint8_t type);
void net_frame_end(struct net_buf *b, size_t start);
void net_put_u8(struct net_buf *b, uint8_t v);
void net_put_u16(struct net_buf *b, uint16_t v);
void net_put_u32(struct net_buf *b, uint32_t v);
void net_put_utf8(struct net_buf *b, uint32_t cp);
void net_put_wide(struct net_buf *b, const wchar_t *s, int n);

/* Messages */
void net_msg_hello(struct net_buf *b, const struct net_hello *h);
void net_msg_term_open(struct net_buf *b, int term, int w, int h,
		const char *name);
void net_msg_palette(struct net_buf *b, const uint8_t (*rgb)[3], int n);
void net_msg_text(struct net_buf *b, int term, int x, int y, int attr,
		const wchar_t *s, int n);
void net_msg_wipe(struct net_buf *b, int term, int x, int y, int n);
void net_msg_clear(struct net_buf *b, int term);
void net_msg_cursor(struct net_buf *b, int term, int x, int y, int flags);
void net_msg_fresh(struct net_buf *b, int term);
void net_msg_bell(struct net_buf *b);
void net_msg_bye(struct net_buf *b, const char *reason);
void net_msg_tileset(struct net_buf *b, const struct net_tileset *t);
void net_msg_pict(struct net_buf *b, int term, int x, int y,
		const struct net_pict *cells, int n);
void net_msg_key(struct net_buf *b, const struct net_key *k);
void net_msg_mouse(struct net_buf *b, const struct net_mouse *m);
void net_msg_resize(struct net_buf *b, const struct net_resize *r);
void net_msg_quit(struct net_buf *b);

/* Decoding */
int net_frame_next(const uint8_t *data, size_t len, struct net_frame *f);
void net_reader_init(struct net_reader *r, const struct net_frame *f);
uint8_t net_get_u8(struct net_reader *r);
uint16_t net_get_u16(struct net_reader *r);
uint32_t net_get_u32(struct net_reader *r);
bool net_parse_hello(const struct net_frame *f, struct net_hello *h);
bool net_parse_key(const struct net_frame *f, struct net_key *k);
bool net_parse_mouse(const struct net_frame *f, struct net_mouse *m);
bool net_parse_resize(const struct net_frame *f, struct net_resize *r);

#endif /* INCLUDED_NET_PROTO_H */
