/* net/proto.c */
/*
 * Check frame encoding and decoding for the net front end against the
 * shared vectors in proto/vectors.txt.  The Go client's proto package
 * checks the same vectors, so the two sides agree on every byte.
 */

#include "unit-test.h"
#include "net-proto.h"

#include <stdio.h>
#include <string.h>

#ifndef NET_PROTO_VECTORS
#error "NET_PROTO_VECTORS must name proto/vectors.txt"
#endif

#define MAX_VECTOR 256

struct vector {
	uint8_t bytes[MAX_VECTOR];
	size_t len;
};

struct state {
	struct net_buf buf;
};

int setup_tests(void **state)
{
	struct state *s = calloc(1, sizeof(*s));

	if (!s) return 1;
	net_buf_init(&s->buf);
	*state = s;
	return 0;
}

int teardown_tests(void *state)
{
	struct state *s = state;

	net_buf_free(&s->buf);
	free(s);
	return 0;
}

static int hex_digit(char c)
{
	if (c >= '0' && c <= '9') return c - '0';
	if (c >= 'a' && c <= 'f') return c - 'a' + 10;
	if (c >= 'A' && c <= 'F') return c - 'A' + 10;
	return -1;
}

/**
 * Look up a vector by name in proto/vectors.txt.
 */
static bool load_vector(const char *name, struct vector *v)
{
	char line[1024];
	size_t name_len = strlen(name);
	bool found = false;
	FILE *f = fopen(NET_PROTO_VECTORS, "r");

	if (!f) return false;
	while (!found && fgets(line, sizeof(line), f)) {
		const char *p;

		if (strncmp(line, name, name_len) || line[name_len] != ' ') continue;
		v->len = 0;
		for (p = line + name_len + 1; hex_digit(p[0]) >= 0
				&& hex_digit(p[1]) >= 0 && v->len < MAX_VECTOR; p += 2) {
			v->bytes[v->len++] = (uint8_t)(hex_digit(p[0]) * 16
				+ hex_digit(p[1]));
		}
		found = true;
	}
	fclose(f);
	return found;
}

/**
 * Check that the buffer holds exactly the named vector, then empty it.
 */
static bool buf_matches(struct net_buf *b, const char *name)
{
	struct vector v;
	bool same;

	if (!load_vector(name, &v)) {
		if (verbose) printf("    no vector named %s\n", name);
		return false;
	}
	same = (b->len == v.len) && !memcmp(b->data, v.bytes, v.len);
	if (!same && verbose) {
		size_t i;

		printf("    %s: got ", name);
		for (i = 0; i < b->len; i++) printf("%02x", b->data[i]);
		printf("\n");
	}
	b->len = 0;
	return same;
}

static void hello_client(struct net_hello *h)
{
	memset(h, 0, sizeof(*h));
	h->version = 1;
	my_strcpy(h->name, "fancyband-tui", sizeof(h->name));
	h->n_terms = 2;
	h->terms[0].index = 0;
	h->terms[0].width = 80;
	h->terms[0].height = 24;
	h->terms[1].index = 1;
	h->terms[1].width = 80;
	h->terms[1].height = 10;
	h->n_caps = 1;
	my_strcpy(h->caps[0], "events", sizeof(h->caps[0]));
}

static int test_encode_server_messages(void *state)
{
	struct state *s = state;
	struct net_buf *b = &s->buf;
	struct net_hello h;
	const uint8_t rgb[2][3] = { { 0, 0, 0 }, { 255, 128, 1 } };
	const wchar_t ascii[] = { L'@', L'#', L'.' };
	const wchar_t wide[] = { 0xE9, 0xB7, 0x2588, 0x1D11E };
	const wchar_t bad[] = { 0xD800, 0x110000 };

	memset(&h, 0, sizeof(h));
	h.version = 1;
	my_strcpy(h.name, "Fancyband 4.2.6", sizeof(h.name));
	net_msg_hello(b, &h);
	require(buf_matches(b, "hello_server"));

	net_msg_term_open(b, 1, 80, 10, "Term-1");
	require(buf_matches(b, "term_open"));

	net_msg_palette(b, rgb, 2);
	require(buf_matches(b, "palette"));

	net_msg_text(b, 0, 3, 2, 0x010F, ascii, 3);
	require(buf_matches(b, "text_ascii"));

	net_msg_text(b, 2, 0, 0, 0x0281, wide, 4);
	require(buf_matches(b, "text_utf8"));

	net_msg_text(b, 0, 0, 0, 1, bad, 2);
	require(buf_matches(b, "text_bad_codepoint"));

	net_msg_wipe(b, 0, 10, 23, 70);
	require(buf_matches(b, "wipe"));

	net_msg_clear(b, 3);
	require(buf_matches(b, "clear"));

	net_msg_cursor(b, 0, 13, 0, NET_CURSOR_VISIBLE);
	require(buf_matches(b, "cursor"));

	net_msg_fresh(b, 0);
	require(buf_matches(b, "fresh"));

	net_msg_bell(b);
	require(buf_matches(b, "bell"));

	net_msg_bye(b, "quit");
	require(buf_matches(b, "bye"));

	{
		struct net_tileset ts = { 2, 16, 16, 2, 1, 0, "Adam Bolt's tiles",
			"/lib/tiles/adam-bolt/16x16.png" };
		const struct net_pict cells[2] = { { 0, 6, 0, 6 }, { 3, 4, 0, 6 } };

		net_msg_tileset(b, &ts);
		require(buf_matches(b, "tileset"));
		net_msg_pict(b, 0, 20, 5, cells, 2);
		require(buf_matches(b, "pict"));
	}
	ok;
}

static int test_encode_client_messages(void *state)
{
	struct state *s = state;
	struct net_buf *b = &s->buf;
	struct net_hello h;
	struct net_key esc = { 0xE000, 0 };
	struct net_key kp8 = { '8', 0x10 };
	struct net_mouse m = { 0, 40, 12, 1, 2 };
	struct net_resize r = { 0, 120, 40 };

	hello_client(&h);
	net_msg_hello(b, &h);
	require(buf_matches(b, "hello_client"));

	net_msg_key(b, &esc);
	require(buf_matches(b, "key_escape"));

	net_msg_key(b, &kp8);
	require(buf_matches(b, "key_keypad_8"));

	net_msg_mouse(b, &m);
	require(buf_matches(b, "mouse"));

	net_msg_resize(b, &r);
	require(buf_matches(b, "resize"));

	net_msg_quit(b);
	require(buf_matches(b, "quit"));
	ok;
}

static int test_decode_client_messages(void *state)
{
	struct vector v;
	struct net_frame f;
	struct net_hello h, want;
	struct net_key k;
	struct net_mouse m;
	struct net_resize r;

	require(load_vector("hello_client", &v));
	eq(net_frame_next(v.bytes, v.len, &f), (int)v.len);
	require(net_parse_hello(&f, &h));
	hello_client(&want);
	eq(h.version, want.version);
	require(streq(h.name, want.name));
	eq(h.n_terms, 2);
	eq(h.terms[1].index, 1);
	eq(h.terms[1].width, 80);
	eq(h.terms[1].height, 10);
	eq(h.n_caps, 1);
	require(streq(h.caps[0], "events"));

	require(load_vector("key_keypad_8", &v));
	eq(net_frame_next(v.bytes, v.len, &f), (int)v.len);
	require(net_parse_key(&f, &k));
	eq(k.code, '8');
	eq(k.mods, 0x10);

	require(load_vector("mouse", &v));
	eq(net_frame_next(v.bytes, v.len, &f), (int)v.len);
	require(net_parse_mouse(&f, &m));
	eq(m.x, 40);
	eq(m.y, 12);
	eq(m.button, 1);
	eq(m.mods, 2);

	require(load_vector("resize", &v));
	eq(net_frame_next(v.bytes, v.len, &f), (int)v.len);
	require(net_parse_resize(&f, &r));
	eq(r.term, 0);
	eq(r.width, 120);
	eq(r.height, 40);

	/* A frame of the wrong type does not parse as a key */
	require(!net_parse_key(&f, &k));
	ok;
}

static int test_frame_boundaries(void *state)
{
	struct vector v;
	struct net_frame f;
	struct net_key k;
	const uint8_t zero_len[] = { 0, 0, 0, 0, 0x40 };
	const uint8_t huge[] = { 0, 0, 0x20, 0, 0x40 };
	const uint8_t short_key[] = { 3, 0, 0, 0, 0x40, 1, 2 };

	/* Every prefix of a frame is incomplete */
	require(load_vector("key_escape", &v));
	for (size_t i = 0; i < v.len; i++) {
		eq(net_frame_next(v.bytes, i, &f), 0);
	}

	/* Lengths of zero or past the limit poison the stream */
	eq(net_frame_next(zero_len, sizeof(zero_len), &f), -1);
	eq(net_frame_next(huge, sizeof(huge), &f), -1);

	/* A key frame with too short a payload fails to parse */
	eq(net_frame_next(short_key, sizeof(short_key), &f),
		(int)sizeof(short_key));
	require(!net_parse_key(&f, &k));
	ok;
}

static int test_buf_consume(void *state)
{
	struct state *s = state;
	struct net_buf *b = &s->buf;

	net_buf_append(b, "abcdef", 6);
	net_buf_consume(b, 2);
	eq(b->len, 4);
	require(!memcmp(b->data, "cdef", 4));
	net_buf_consume(b, 10);
	eq(b->len, 0);
	ok;
}

const char *suite_name = "net/proto";
struct test tests[] = {
	{ "encode server messages", test_encode_server_messages },
	{ "encode client messages", test_encode_client_messages },
	{ "decode client messages", test_decode_client_messages },
	{ "frame boundaries", test_frame_boundaries },
	{ "buffer consume", test_buf_consume },
	{ NULL, NULL }
};
