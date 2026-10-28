/**
 * \file main-net.c
 * \brief Stream the game's terms to a client over a socket
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
 * This module draws nothing itself.  It sends each term's updates to a
 * client as frames and turns the client's frames into key presses.  The
 * client is fancyband-tui, or netdump when debugging.  The protocol is in
 * proto/README.md.
 *
 * The game stays single-threaded: it reads from the socket only when it
 * asks for input through TERM_XTRA_EVENT.
 */
#include "angband.h"
#include "buildid.h"
#include "game-world.h"
#include "grafmode.h"
#include "main.h"
#include "net-frontend.h"
#include "net-proto.h"
#include "player.h"
#include "savefile.h"
#include "ui-command.h"
#include "ui-display.h"
#include "ui-game.h"
#include "ui-prefs.h"
#include "ui-term.h"

#ifdef USE_NET

#include <errno.h>
#include <fcntl.h>
#include <limits.h>
#include <stdlib.h>
#include <poll.h>
#include <signal.h>
#include <sys/socket.h>
#include <sys/stat.h>
#include <sys/un.h>
#include <unistd.h>

/**
 * How long a blocking wait for input lasts before the game gets a chance
 * to animate, in milliseconds.  main-gcu.c uses the same interval.
 */
#define NET_IDLE_MS 200

/**
 * Send buffered output once it grows past this many bytes, even before
 * the end of a frame.
 */
#define NET_FLUSH_AT (64 * 1024)

/**
 * The smallest main term the game can use, as in main-gcu.c, and the
 * largest term the client may ask for in each direction.
 */
#define MIN_TERM0_LINES 24
#define MIN_TERM0_COLS 80
#define NET_TERM_MAX_SIZE 1000

typedef struct term_data term_data;
struct term_data {
	term t;
	int index;

	/* The cursor as last set by the game, and whether the client has it */
	int cx, cy;
	bool cursor_visible;
	bool cursor_big;
	bool cursor_dirty;

	/* Whether anything was sent since the last FRESH */
	bool drawn;
};

static term_data data[ANGBAND_TERM_MAX];

/* The connection to the client */
static int net_fd = -1;

/* Frames waiting to be sent, and bytes received but not yet handled */
struct net_buf net_out;
static struct net_buf net_in;

/* Running with no text UI and no terms; see net-game.c */
bool net_headless = false;

/* The client has gone, or sending to it failed */
static bool net_closed = false;

/* The game is saving and exiting; every prompt now gets Escape */
static bool net_exiting = false;

/* Why the game is exiting, sent to the client in BYE */
static const char *net_bye_reason = "exit";

const char help_net[] = "Stream to a client, subopts -- --fd N | --listen PATH [--headless]";

/**
 * ------------------------------------------------------------------------
 * Sending and receiving
 * ------------------------------------------------------------------------ */

/**
 * Send all buffered output.  A failure marks the connection closed; the
 * game notices the next time it waits for input.
 */
void net_flush(void)
{
	size_t sent = 0;

	if (net_closed) {
		net_out.len = 0;
		return;
	}

	while (sent < net_out.len) {
#ifdef MSG_NOSIGNAL
		ssize_t n = send(net_fd, net_out.data + sent, net_out.len - sent,
			MSG_NOSIGNAL);
#else
		ssize_t n = send(net_fd, net_out.data + sent, net_out.len - sent,
			0);
#endif

		if (n < 0) {
			if (errno == EINTR) continue;
			net_closed = true;
			break;
		}
		sent += (size_t)n;
	}
	net_out.len = 0;
}

static void net_maybe_flush(void)
{
	if (net_out.len >= NET_FLUSH_AT) net_flush();
}

/**
 * Read whatever the socket has, waiting up to timeout_ms (or forever if
 * negative).  Returns false when the client has gone.
 */
static bool net_fill(int timeout_ms, bool *got)
{
	struct pollfd pfd;
	uint8_t chunk[4096];
	ssize_t n;
	int ready;

	*got = false;
	pfd.fd = net_fd;
	pfd.events = POLLIN;
	pfd.revents = 0;
	ready = poll(&pfd, 1, timeout_ms);
	if (ready < 0) return errno == EINTR;
	if (ready == 0) return true;

	n = recv(net_fd, chunk, sizeof(chunk), 0);
	if (n < 0) return errno == EINTR || errno == EAGAIN;
	if (n == 0) return false;

	net_buf_append(&net_in, chunk, (size_t)n);
	*got = true;
	return true;
}

/**
 * ------------------------------------------------------------------------
 * Signals
 * ------------------------------------------------------------------------ */

/**
 * The interrupt or terminate signal that arrived, or 0.
 */
static volatile sig_atomic_t net_signalled = 0;

static void net_on_signal(int sig)
{
	net_signalled = sig;
}

/**
 * Catch signals the way a game with no terminal of its own needs to.
 * Angband's own handlers write a panic save on SIGTERM and, after a few
 * SIGINTs, kill the character; headless mode never installs them at all,
 * so SIGHUP would end the game unsaved.  Here a SIGHUP, as when an SSH
 * session dies, is ignored: the client dies with the terminal, and the
 * closed connection saves the game.  SIGINT and SIGTERM save and exit at
 * the next read (see net_read()).  This runs at the first read, after
 * main.c's signals_init().
 */
static void net_catch_signals(void)
{
	static bool done = false;
	struct sigaction sa;

	if (done) return;
	done = true;

	memset(&sa, 0, sizeof(sa));
	sigemptyset(&sa.sa_mask);
	sa.sa_handler = SIG_IGN;
	sigaction(SIGHUP, &sa, NULL);

	/* No SA_RESTART, so a signal wakes poll() at once */
	sa.sa_handler = net_on_signal;
	sigaction(SIGINT, &sa, NULL);
	sigaction(SIGTERM, &sa, NULL);
}

/**
 * ------------------------------------------------------------------------
 * Leaving the game
 * ------------------------------------------------------------------------ */

/**
 * Save if there is anything to save, then exit.  The client has gone or
 * asked to quit, so the game cannot ask it anything.  While saving, every
 * prompt the game shows gets Escape (see net_event()).
 *
 * A character still being created is not saved.  A dead character is
 * saved as dead, as close_game() would, so that dropping the connection
 * on the death screen cannot bring the character back.
 */
void net_save_and_exit(const char *reason)
{
	if (net_exiting) return;
	net_exiting = true;
	net_bye_reason = reason;

	if (character_generated) {
		if (player->is_dead || net_headless) {
			/* save_game_checked() draws on the screen, which headless
			 * mode does not have */
			if (!savefile_save(savefile)) plog("Saving the game failed.");
		} else if (!save_game_checked()) {
			plog("Saving the game failed.");
		}
	}

	quit(NULL);
}

/**
 * Exit without saving, telling the client why.
 */
void net_exit(const char *reason)
{
	net_exiting = true;
	net_bye_reason = reason;
	quit(NULL);
}

bool net_is_exiting(void)
{
	return net_exiting;
}

/**
 * ------------------------------------------------------------------------
 * Handling client frames
 * ------------------------------------------------------------------------ */

/**
 * Send the color table.  The game calls TERM_XTRA_REACT after the player
 * edits colors, so this runs then as well as at start-up.
 */
static void net_send_palette(void)
{
	uint8_t rgb[MAX_COLORS][3];
	int i;

	for (i = 0; i < MAX_COLORS; i++) {
		rgb[i][0] = angband_color_table[i][1];
		rgb[i][1] = angband_color_table[i][2];
		rgb[i][2] = angband_color_table[i][3];
	}
	net_msg_palette(&net_out, (const uint8_t (*)[3])rgb, MAX_COLORS);
}

/**
 * Keep a term size within what the game supports.
 */
static void net_clamp_size(int index, int *w, int *h)
{
	int min_w = (index == 0) ? MIN_TERM0_COLS : 1;
	int min_h = (index == 0) ? MIN_TERM0_LINES : 1;

	*w = MAX(min_w, MIN(*w, NET_TERM_MAX_SIZE));
	*h = MAX(min_h, MIN(*h, NET_TERM_MAX_SIZE));
}

static void net_resize(const struct net_resize *rs)
{
	term *old = Term;
	int w = rs->width, h = rs->height;
	int i = rs->term;

	if (i >= ANGBAND_TERM_MAX || !angband_term[i]) return;
	net_clamp_size(i, &w, &h);

	/* Tell the client the size it got, which may differ from the request */
	net_msg_term_open(&net_out, i, w, h, angband_term_name[i]);

	Term_activate(&data[i].t);
	Term_resize(w, h);
	Term_activate(old);
	do_cmd_redraw();
}

/**
 * Act on one frame from the client.  Unknown types are ignored, so a
 * newer client can talk to this server.
 */
static void net_handle_frame(const struct net_frame *f)
{
	struct net_key k;
	struct net_mouse m;
	struct net_resize rs;

	switch (f->type) {
		case NET_KEY:
			if (!net_headless && net_parse_key(f, &k)) {
				Term_keypress(k.code, k.mods);
			}
			break;

		case NET_MOUSE:
			/* Only the main term takes mouse input, as in main-gcu.c */
			if (!net_headless && net_parse_mouse(f, &m) && m.term == 0) {
				Term_mousepress(m.x, m.y,
					(char)((m.button & 0x0F) | ((m.mods & 0x0F) << 4)));
			}
			break;

		case NET_RESIZE:
			if (!net_headless && net_parse_resize(f, &rs)) net_resize(&rs);
			break;

		case NET_COMMAND:
		case NET_REPLY:
			net_game_frame(f);
			break;

		case NET_QUIT:
			net_save_and_exit("quit");
			break;

		default:
			break;
	}
}

/**
 * Handle every complete frame received so far.  Returns false if the
 * stream is corrupt.
 */
static bool net_handle_input(void)
{
	size_t used = 0;

	while (used < net_in.len) {
		struct net_frame f;
		int n = net_frame_next(net_in.data + used, net_in.len - used, &f);

		if (n < 0) return false;
		if (n == 0) break;
		net_handle_frame(&f);
		used += (size_t)n;

		/* A frame may have started the exit; stop reading if so */
		if (net_exiting) break;
	}
	net_buf_consume(&net_in, used);
	return true;
}

/**
 * Handle the frames that have arrived, waiting for some if wait is true.
 * Returns whether any arrived.  A dropped connection saves and exits.
 */
static bool net_read(bool wait)
{
	bool got;

	net_catch_signals();
	net_flush();
	if (net_closed) net_save_and_exit("disconnected");

	while (true) {
		if (net_signalled) net_save_and_exit("terminated");
		if (!net_fill(wait ? NET_IDLE_MS : 0, &got)) {
			net_closed = true;
			net_save_and_exit("disconnected");
		}
		if (net_signalled) net_save_and_exit("terminated");
		if (got) break;
		if (!wait) return false;

		/* Nothing yet; let the game animate, as main-gcu.c does */
		if (!net_headless) idle_update();
		net_flush();
		if (net_closed) net_save_and_exit("disconnected");
	}

	if (!net_handle_input()) {
		net_closed = true;
		net_save_and_exit("protocol error");
	}
	return true;
}

/**
 * Wait for at least one frame from the client and handle it.
 */
void net_wait_input(void)
{
	(void)net_read(true);
}

/**
 * Process input, waiting for it if wait is true.  Returns 0 if a frame
 * was handled and 1 if none was waiting, as TERM_XTRA_EVENT expects.
 */
static errr net_event(bool wait)
{
	/* Answer every prompt with Escape while saving on the way out */
	if (net_exiting) {
		if (!wait) return 1;
		Term_keypress(ESCAPE, 0);
		return 0;
	}

	if (wait) net_game_key_wait();
	return net_read(wait) ? 0 : 1;
}

/**
 * ------------------------------------------------------------------------
 * Term hooks
 * ------------------------------------------------------------------------ */

static void net_send_cursor(term_data *td)
{
	int flags = 0;

	if (!td->cursor_dirty) return;
	if (td->cursor_visible) flags |= NET_CURSOR_VISIBLE;
	if (td->cursor_big) flags |= NET_CURSOR_BIG;
	net_msg_cursor(&net_out, td->index, td->cx, td->cy, flags);
	td->cursor_dirty = false;
	td->drawn = true;
}

static errr Term_xtra_net(int n, int v)
{
	term_data *td = (term_data *)(Term->data);

	switch (n) {
		case TERM_XTRA_CLEAR:
			net_msg_clear(&net_out, td->index);
			td->drawn = true;
			return 0;

		case TERM_XTRA_NOISE:
			net_msg_bell(&net_out);
			return 0;

		case TERM_XTRA_FRESH:
			/* The game refreshes far more often than it draws: a move
			 * makes 20 to 40 refreshes, most with nothing new.  Send a
			 * FRESH only after a change, and leave the sending to
			 * net_read() and TERM_XTRA_DELAY, so a move's frames go out
			 * together just before the game waits for the next key. */
			net_send_cursor(td);
			if (td->drawn) {
				net_msg_fresh(&net_out, td->index);
				td->drawn = false;
			}
			return 0;

		case TERM_XTRA_SHAPE:
			if (td->cursor_visible != (v != 0)) {
				td->cursor_visible = (v != 0);
				td->cursor_dirty = true;
			}
			return 0;

		case TERM_XTRA_EVENT:
			return net_event(v != 0);

		case TERM_XTRA_FLUSH:
			while (!net_event(false));
			return 0;

		case TERM_XTRA_DELAY:
			/* Show what is drawn so far, so animations are visible */
			net_flush();
			if (v > 0) usleep(1000 * v);
			return 0;

		case TERM_XTRA_REACT:
			net_send_palette();
			return 0;

		case TERM_XTRA_ALIVE:
			return 0;
	}

	return 1;
}

static void net_move_cursor(term_data *td, int x, int y, bool big)
{
	if (td->cx != x || td->cy != y || td->cursor_big != big) {
		td->cx = x;
		td->cy = y;
		td->cursor_big = big;
		td->cursor_dirty = true;
	}
}

static errr Term_curs_net(int x, int y)
{
	net_move_cursor((term_data *)(Term->data), x, y, false);
	return 0;
}

static errr Term_bigcurs_net(int x, int y)
{
	net_move_cursor((term_data *)(Term->data), x, y, true);
	return 0;
}

static errr Term_wipe_net(int x, int y, int n)
{
	term_data *td = (term_data *)(Term->data);

	net_msg_wipe(&net_out, td->index, x, y, n);
	td->drawn = true;
	net_maybe_flush();
	return 0;
}

/**
 * Send a run of tiles.  Bit 7 marks a tile in both the attribute and the
 * character; the rest is the tile's row and column in the sheet.
 */
static errr Term_pict_net(int x, int y, int n, const int *ap,
		const wchar_t *cp, const int *tap, const wchar_t *tcp)
{
	term_data *td = (term_data *)(Term->data);
	struct net_pict cells[256];
	int i, done = 0;

	while (done < n) {
		int run = MIN(n - done, (int)N_ELEMENTS(cells));

		for (i = 0; i < run; i++) {
			cells[i].fg_row = ap[done + i] & 0x7F;
			cells[i].fg_col = cp[done + i] & 0x7F;
			cells[i].bg_row = tap[done + i] & 0x7F;
			cells[i].bg_col = tcp[done + i] & 0x7F;
		}
		net_msg_pict(&net_out, td->index, x + done, y, cells, run);
		done += run;
	}
	td->drawn = true;
	net_maybe_flush();
	return 0;
}

static errr Term_text_net(int x, int y, int n, int a, const wchar_t *s)
{
	term_data *td = (term_data *)(Term->data);

	net_msg_text(&net_out, td->index, x, y, a, s, n);
	td->drawn = true;
	net_maybe_flush();
	return 0;
}

/**
 * ------------------------------------------------------------------------
 * Start-up and shutdown
 * ------------------------------------------------------------------------ */

static void term_data_init_net(int index, int w, int h)
{
	term_data *td = &data[index];
	term *t = &td->t;

	memset(td, 0, sizeof(*td));
	td->index = index;
	td->cursor_dirty = true;

	term_init(t, w, h, 256);

	/* Tell Backspace from ^h, Tab from ^i, and so on */
	t->complex_input = true;

	t->text_hook = Term_text_net;
	t->wipe_hook = Term_wipe_net;
	t->curs_hook = Term_curs_net;
	t->bigcurs_hook = Term_bigcurs_net;
	t->xtra_hook = Term_xtra_net;
	t->data = td;

	Term_activate(t);
	angband_term[index] = t;
	net_msg_term_open(&net_out, index, w, h, angband_term_name[index]);
}

static void hook_quit(const char *str)
{
	int i;

	if (net_fd >= 0) {
		net_msg_bye(&net_out, str ? str : net_bye_reason);
		net_flush();
	}

	for (i = ANGBAND_TERM_MAX - 1; i >= 0; i--) {
		if (angband_term[i]) term_nuke(angband_term[i]);
	}

	if (net_fd >= 0) close(net_fd);
	net_fd = -1;
	net_buf_free(&net_out);
	net_buf_free(&net_in);
}

/**
 * Wait on a Unix socket at path for one client, then stop listening.
 * Returns the connection, or -1.
 */
static int net_listen(const char *path)
{
	struct sockaddr_un addr;
	struct stat st;
	int lfd, fd;

	if (strlen(path) >= sizeof(addr.sun_path)) {
		plog_fmt("Socket path is too long: %s", path);
		return -1;
	}

	/* Replace a stale socket from an earlier run, but nothing else */
	if (lstat(path, &st) == 0) {
		if (!S_ISSOCK(st.st_mode)) {
			plog_fmt("%s exists and is not a socket", path);
			return -1;
		}
		unlink(path);
	}

	lfd = socket(AF_UNIX, SOCK_STREAM, 0);
	if (lfd < 0) return -1;

	memset(&addr, 0, sizeof(addr));
	addr.sun_family = AF_UNIX;
	my_strcpy(addr.sun_path, path, sizeof(addr.sun_path));
	if (bind(lfd, (struct sockaddr *)&addr, sizeof(addr)) < 0
			|| listen(lfd, 1) < 0) {
		plog_fmt("Cannot listen on %s: %s", path, strerror(errno));
		close(lfd);
		return -1;
	}

	fprintf(stderr, "Waiting for a client on %s\n", path);
	do {
		fd = accept(lfd, NULL, NULL);
	} while (fd < 0 && errno == EINTR);

	close(lfd);
	unlink(path);
	return fd;
}

/**
 * Read frames until the client's HELLO arrives.
 */
static bool net_read_hello(struct net_hello *hello)
{
	while (true) {
		struct net_frame f;
		bool got;
		int n = net_frame_next(net_in.data, net_in.len, &f);

		if (n < 0) return false;
		if (n > 0) {
			bool ok = net_parse_hello(&f, hello);

			net_buf_consume(&net_in, (size_t)n);
			return ok;
		}
		if (!net_fill(-1, &got)) return false;
	}
}

/**
 * Draw the map with tile set id, if the client asked for one with a
 * "tiles:N" capability and the set exists.  The engine loads the set's
 * preference files when it leaves start-up (reset_visuals() in
 * ui-display.c), since use_graphics is set by then.
 */
static void net_use_tiles(const struct net_hello *hello)
{
	graphics_mode *mode;
	struct net_tileset ts;
	char sheet[1024];
	char abs_sheet[PATH_MAX];
	int i, id = 0;

	for (i = 0; i < hello->n_caps; i++) {
		if (prefix(hello->caps[i], "tiles:")) id = atoi(hello->caps[i] + 6);
	}
	if (id <= 0) return;

	if (!init_graphics_modes()) {
		plog("net: cannot read the list of tile sets");
		return;
	}
	mode = get_graphics_mode(id);
	if (!mode || mode->grafID == GRAPHICS_NONE) {
		plog_fmt("net: no tile set %d", id);
		return;
	}
	path_build(sheet, sizeof(sheet), mode->path, mode->file);
	if (!realpath(sheet, abs_sheet)) {
		plog_fmt("net: cannot find the tile sheet %s", sheet);
		return;
	}

	use_graphics = mode->grafID;
	current_graphics_mode = mode;

	/* Terminal cells are about twice as tall as they are wide, so a
	 * square tile covers two cells side by side */
	tile_width = MAX(1, (2 * mode->cell_width + mode->cell_height / 2)
		/ mode->cell_height);
	tile_height = 1;

	data[0].t.higher_pict = true;
	data[0].t.pict_hook = Term_pict_net;

	memset(&ts, 0, sizeof(ts));
	ts.id = mode->grafID;
	ts.tile_w = mode->cell_width;
	ts.tile_h = mode->cell_height;
	ts.cells_w = tile_width;
	ts.cells_h = tile_height;
	ts.alpha = mode->alphablend;
	ts.name = mode->menuname;
	ts.path = abs_sheet;
	net_msg_tileset(&net_out, &ts);
}

/**
 * Open the terms the client asked for.  The main term is always opened.
 */
static void net_open_terms(const struct net_hello *hello)
{
	bool want[ANGBAND_TERM_MAX] = { false };
	int w[ANGBAND_TERM_MAX], h[ANGBAND_TERM_MAX];
	int i;

	want[0] = true;
	w[0] = MIN_TERM0_COLS;
	h[0] = MIN_TERM0_LINES;

	for (i = 0; i < hello->n_terms; i++) {
		const struct net_term_size *ts = &hello->terms[i];

		if (ts->index >= ANGBAND_TERM_MAX) continue;
		want[ts->index] = true;
		w[ts->index] = ts->width;
		h[ts->index] = ts->height;
	}

	/* Open the subwindows first so the main term ends up active */
	for (i = ANGBAND_TERM_MAX - 1; i >= 0; i--) {
		if (!want[i]) continue;
		net_clamp_size(i, &w[i], &h[i]);
		term_data_init_net(i, w[i], h[i]);
	}
}

errr init_net(int argc, char **argv)
{
	struct net_hello hello, reply;
	const char *listen_path = NULL;
	int fd = -1;
	int i;

	for (i = 1; i < argc; i++) {
		if (streq(argv[i], "--fd") && i + 1 < argc) {
			fd = atoi(argv[++i]);
		} else if (streq(argv[i], "--listen") && i + 1 < argc) {
			listen_path = argv[++i];
		} else if (streq(argv[i], "--headless")) {
			net_headless = true;
		} else {
			plog_fmt("net: unknown option %s", argv[i]);
			return -1;
		}
	}

	if (listen_path) {
		fd = net_listen(listen_path);
	} else if (fd < 0) {
		plog("net: pass -- --fd N or -- --listen PATH");
		return -1;
	}
	if (fd < 0) return -1;

	/* Keep the socket out of anything the game might run */
	(void)fcntl(fd, F_SETFD, FD_CLOEXEC);
#ifdef SO_NOSIGPIPE
	{
		int one = 1;
		(void)setsockopt(fd, SOL_SOCKET, SO_NOSIGPIPE, &one, sizeof(one));
	}
#endif

	net_fd = fd;
	net_buf_init(&net_out);
	net_buf_init(&net_in);

	if (!net_read_hello(&hello)) {
		plog("net: the client did not send HELLO");
		close(fd);
		net_fd = -1;
		return -1;
	}

	memset(&reply, 0, sizeof(reply));
	reply.version = NET_PROTO_VERSION;
	my_strcpy(reply.name, buildid, sizeof(reply.name));
	net_msg_hello(&net_out, &reply);

	if (hello.version != NET_PROTO_VERSION) {
		net_msg_bye(&net_out, format("protocol version %d is not supported",
			hello.version));
		net_flush();
		close(fd);
		net_fd = -1;
		quit_fmt("net: the client speaks protocol version %d, not %d",
			hello.version, NET_PROTO_VERSION);
	}

	quit_aux = hook_quit;
	net_game_hello(&hello);

	if (net_headless) {
		/* No terms: the game runs from net-game.c instead of the text UI */
		frontend_play_hook = net_game_play;
	} else {
		net_open_terms(&hello);
		net_send_palette();
		net_use_tiles(&hello);
		frontend_post_init_hook = net_game_install;
	}
	net_flush();

	return 0;
}

#endif /* USE_NET */
