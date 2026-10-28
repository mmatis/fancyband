/* net/json.c */
/*
 * Check the JSON writer and parser that the net front end uses for its
 * structured messages.
 */

#include "unit-test.h"
#include "net-json.h"

#include <string.h>

NOSETUP
NOTEARDOWN

/* Compare a buffer's bytes with a string. */
static bool buf_is(const struct net_buf *b, const char *want)
{
	if (b->len != strlen(want) || memcmp(b->data, want, b->len)) {
		if (verbose) printf("    got %.*s\n", (int)b->len, (const char *)b->data);
		return false;
	}
	return true;
}

static int test_write(void *state)
{
	struct net_buf b;
	struct net_jw w;

	net_buf_init(&b);
	net_jw_init(&w, &b);
	net_jw_obj(&w);
	net_jw_kstr(&w, "event", "HP");
	net_jw_kint(&w, "hp", -3);
	net_jw_key(&w, "list");
	net_jw_arr(&w);
	net_jw_int(&w, 1);
	net_jw_obj(&w);
	net_jw_obj_end(&w);
	net_jw_bool(&w, true);
	net_jw_null(&w);
	net_jw_arr_end(&w);
	net_jw_kstr(&w, "text", "a \"b\"\\\n\x01 \xc3\xa9");
	net_jw_obj_end(&w);
	require(buf_is(&b, "{\"event\":\"HP\",\"hp\":-3,\"list\":[1,{},true,null],"
		"\"text\":\"a \\\"b\\\"\\\\\\n\\u0001 \xc3\xa9\"}"));
	net_buf_free(&b);
	ok;
}

static int test_parse(void *state)
{
	const char *text = " {\"cmd\": \"WALK\", \"args\": {\"direction\": 6,"
		" \"item\": {\"inven\": 2}}, \"n\": -1.5, \"ok\": true,"
		" \"list\": [1, \"x\", null], \"s\": \"\\u00e9\\ud834\\udd1e\\n\"} ";
	struct net_json *v = net_json_parse(text, strlen(text));
	struct net_json *args, *list;
	int n;
	bool flag;

	require(v);
	require(streq(net_json_get_str(v, "cmd"), "WALK"));
	args = net_json_get(v, "args");
	require(net_json_get_int(args, "direction", &n));
	eq(n, 6);
	require(net_json_get_int(net_json_get(args, "item"), "inven", &n));
	eq(n, 2);
	/* -1.5 is not a whole number */
	require(!net_json_get_int(v, "n", &n));
	require(net_json_get_bool(v, "ok", &flag));
	eq(flag, true);
	list = net_json_get(v, "list");
	require(list && list->type == NET_JSON_ARRAY);
	require(list->child && list->child->next && list->child->next->next);
	eq(list->child->next->next->type, NET_JSON_NULL);
	require(streq(net_json_get_str(v, "s"), "\xc3\xa9\xf0\x9d\x84\x9e\n"));
	require(!net_json_get(v, "missing"));
	net_json_free(v);
	ok;
}

static int test_parse_errors(void *state)
{
	const char *bad[] = {
		"", "{", "{\"a\"}", "{\"a\":1,}", "[1 2]", "tru", "\"abc",
		"{\"a\":1} x", "\"\\q\"", "nan", "[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[[1]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]]",
	};
	size_t i;

	for (i = 0; i < N_ELEMENTS(bad); i++) {
		struct net_json *v = net_json_parse(bad[i], strlen(bad[i]));

		if (v && verbose) printf("    parsed %s\n", bad[i]);
		require(!v);
	}
	ok;
}

const char *suite_name = "net/json";
struct test tests[] = {
	{ "write", test_write },
	{ "parse", test_parse },
	{ "parse errors", test_parse_errors },
	{ NULL, NULL }
};
