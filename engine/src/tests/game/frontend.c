/* game/frontend.c */
/*
 * Check the ways a front end plugs into the engine: start-up modules, data
 * binding hooks, front-end command handlers and input fallbacks.  This test
 * links only the engine, so it also shows that the engine runs headless.
 */

#include "unit-test.h"
#include "test-utils.h"

#include "cave.h"
#include "cmd-core.h"
#include "game-input.h"
#include "game-world.h"
#include "generate.h"
#include "init.h"
#include "mon-make.h"
#include "monster.h"
#include "player.h"
#include "player-birth.h"
#include "savefile.h"
#include "target.h"

static int init_counter;
static int before_data_order, after_engine_order;
static int before_data_inits, after_engine_inits;
static bool before_data_saw_monsters, after_engine_saw_monsters;
static int object_bindings;
static int frontend_command_runs;

static void before_data_init(void)
{
	before_data_order = ++init_counter;
	++before_data_inits;
	before_data_saw_monsters = (r_info != NULL);
}

static void after_engine_init(void)
{
	after_engine_order = ++init_counter;
	++after_engine_inits;
	after_engine_saw_monsters = (r_info != NULL);
}

static struct init_module before_data_module = {
	.name = "test-before-data",
	.init = before_data_init,
	.cleanup = NULL
};

static struct init_module after_engine_module = {
	.name = "test-after-engine",
	.init = after_engine_init,
	.cleanup = NULL
};

static int count_object_binding(const char *name, int type, int index,
		int value, bool have_value, bool isaux)
{
	++object_bindings;
	return 0;
}

static void frontend_command(struct command *cmd)
{
	++frontend_command_runs;
}

/* Restart the engine the way the game does for "play again". */
static void restart_engine(void)
{
	play_again = true;
	if (cave) wipe_mon_list(cave, player);
	cleanup_angband();
	chunk_list_max = 0;
	init_angband();
	play_again = false;
}

int setup_tests(void **state)
{
	set_file_paths();
	/* Register twice to check that the second call does nothing. */
	init_add_module(INIT_STAGE_AFTER_ENGINE, &after_engine_module);
	init_add_module(INIT_STAGE_BEFORE_DATA, &before_data_module);
	init_add_module(INIT_STAGE_BEFORE_DATA, &before_data_module);
	init_angband();
#ifdef UNIX
	/* Necessary for creating the randart file. */
	create_needed_dirs();
#endif
	return 0;
}

int teardown_tests(void *state)
{
	file_delete("Test-frontend");
	if (cave) wipe_mon_list(cave, player);
	cleanup_angband();
	return 0;
}

static int test_module_order(void *state)
{
	eq(before_data_inits, 1);
	eq(after_engine_inits, 1);
	require(before_data_order < after_engine_order);
	eq(before_data_saw_monsters, false);
	eq(after_engine_saw_monsters, true);
	ok;
}

static int test_headless_has_no_hooks(void *state)
{
	/* Nothing attached a UI, yet the game data loaded. */
	null(bind_player_ability_hook);
	null(bind_object_property_hook);
	null(set_race_color_cycle_hook);
	null(get_char_hook);
	null(get_target_grid_hook);
	require(z_info->r_max > 0);
	require(z_info->k_max > 0);
	ok;
}

static int test_binding_hook_runs(void *state)
{
	object_bindings = 0;
	bind_object_property_hook = count_object_binding;
	restart_engine();
	bind_object_property_hook = NULL;
	/* object_property.txt has "bindui" lines. */
	require(object_bindings > 0);
	/* Modules ran again, once each. */
	eq(before_data_inits, 2);
	eq(after_engine_inits, 2);
	ok;
}

static int test_input_fallbacks(void *state)
{
	struct loc grid = loc(-1, -1);

	eq(get_char_choice("Pick one? ", "yrn", 3, 'n'), 'n');
	eq(get_target_grid(TARGET_LOOK, &grid), false);
	ok;
}

static int test_frontend_command(void *state)
{
	/* The engine handles walking, so a front end may not replace it. */
	eq(cmd_set_frontend_handler(CMD_WALK, frontend_command), false);

	/* Without a front end, a display-only debug command does nothing. */
	eq(player_make_simple(NULL, NULL, "Tester"), true);
	prepare_next_level(player);
	on_new_level();
	frontend_command_runs = 0;
	cmdq_push(CMD_WIZ_STAT_ITEM);
	cmdq_execute(CTX_GAME);
	eq(frontend_command_runs, 0);

	/* With one supplied, the command runs it. */
	eq(cmd_set_frontend_handler(CMD_WIZ_STAT_ITEM, frontend_command), true);
	cmdq_push(CMD_WIZ_STAT_ITEM);
	cmdq_execute(CTX_GAME);
	eq(frontend_command_runs, 1);
	(void) cmd_set_frontend_handler(CMD_WIZ_STAT_ITEM, NULL);
	ok;
}

static int test_sidebar_mode_saved(void *state)
{
	/* test_frontend_command made a character and a level. */
	player->opts.sidebar_mode = 2;
	eq(savefile_save("Test-frontend"), true);

	restart_engine();
	eq(player->opts.sidebar_mode, 0);
	eq(savefile_load("Test-frontend", false), true);
	eq(player->opts.sidebar_mode, 2);
	ok;
}

const char *suite_name = "game/frontend";
struct test tests[] = {
	{ "module order", test_module_order },
	{ "headless has no hooks", test_headless_has_no_hooks },
	{ "binding hook runs", test_binding_hook_runs },
	{ "input fallbacks", test_input_fallbacks },
	{ "frontend command", test_frontend_command },
	{ "sidebar mode saved", test_sidebar_mode_saved },
	{ NULL, NULL }
};
