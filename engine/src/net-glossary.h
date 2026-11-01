/**
 * \file net-glossary.h
 * \brief The net front end's GLOSSARY
 */
#ifndef INCLUDED_NET_GLOSSARY_H
#define INCLUDED_NET_GLOSSARY_H

struct net_jw;

/**
 * Write "terms", every race, class, stat, skill, ability, element, curse,
 * and status effect with its text and names, and "series", the lists
 * item descriptions name terms in, as keys of the object w is writing.
 * "terms" is null before the game data is loaded.
 */
void net_glossary_write(struct net_jw *w);

#endif /* INCLUDED_NET_GLOSSARY_H */
