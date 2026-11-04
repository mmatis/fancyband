/**
 * \file ui-equip-cmp.h
 * \brief Declare interface for "resistance grid for home"
 *
 * Changed for Fancyband, first on 2026-10-31; FANCYBAND-CHANGES.md in the
 * engine folder says how to find every change and its date.
 */

#ifndef UI_EQUIP_CMP_H
#define UI_EQUIP_CMP_H

void equip_cmp_display(void);

/**
 * What a front end with a comparison screen of its own is told by
 * equip_cmp_describe().
 */
struct equip_cmp_visitor {
	/**
	 * Called for each property column, in the screen's order, with its
	 * category (0 resistances, 1 abilities, 2 hindrances, 3 modifiers,
	 * 4 stat modifiers), its two-letter label, and its full label.
	 */
	void (*prop)(void *closure, int category, const char *code,
		const char *label);
	/**
	 * Called for each item, in the screen's own order, with where it is
	 * ('e' worn, 'p' pack, 'f' floor, 'h' home, 's' store), its rough
	 * quality (0 artifact, 1 ego, 2 good, 3 average, 4 bad), its
	 * equipment slot, and its value for each property.
	 */
	void (*item)(void *closure, const struct object *obj, char source,
		int quality, int slot, const int *vals);
	/**
	 * Called once, before the items, with the values for the player with
	 * their equipment.
	 */
	void (*player)(void *closure, const int *vals);
	void *closure;
};

int equip_cmp_count(void);
int equip_cmp_describe(const struct equip_cmp_visitor *v);
const struct object *equip_cmp_item(int n);

#endif /* !UI_EQUIP_CMP_H */
