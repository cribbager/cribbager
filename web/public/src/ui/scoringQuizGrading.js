// Pure grading logic for the Hand Counting Tutorial (A6). This module is DOM-free
// on purpose so it can be unit-tested under `node --test` without a browser: it
// takes plain declarations + engine combos and returns a graded verdict. The UI
// (scoringQuiz.js) imports these helpers and passes the state in; nothing here
// touches `document` or module-level view state.

import { parseCard, cardCode } from '../engine/cards.js';

// The combo kinds the user can declare — the full vocabulary a player counts
// aloud, in conventional counting order. Compound combos (pair royal, double
// runs, …) are first-class entries: grading reduces BOTH them and the engine's
// bundled combos to the same multiset of ATOMIC components (see grade()), so a
// player may declare a double run either as one bundle or fully decomposed and
// both read Correct. Each entry has an `id` (dropdown value), a `family`
// (fifteen | pair | run | flush | nobs) driving atom generation, a `count`
// (exact card count, or [min,max] for the looser fifteen/flush/nobs) for
// add-time validation, and a `call` (how it's read aloud). Run entries also carry
// a `sig` — the structural signature (base run length + multiplicity) the
// selected cards must actually form for that kind to grade correct (BUG4).
export const KINDS = [
    { id: 'fifteen', label: 'Fifteen', family: 'fifteen', count: [2, 5], call: 'fifteen' },
    { id: 'pair', label: 'Pair', family: 'pair', count: 2, call: 'pair' },
    { id: 'pairroyal', label: 'Pair royal', family: 'pair', count: 3, call: 'pair royal' },
    { id: 'doublepairroyal', label: 'Double pair royal', family: 'pair', count: 4, call: 'double pair royal' },
    { id: 'run3', label: 'Run of 3', family: 'run', count: 3, call: 'run of 3', sig: { runLen: 3, mult: 1 } },
    { id: 'run4', label: 'Run of 4', family: 'run', count: 4, call: 'run of 4', sig: { runLen: 4, mult: 1 } },
    { id: 'run5', label: 'Run of 5', family: 'run', count: 5, call: 'run of 5', sig: { runLen: 5, mult: 1 } },
    { id: 'doublerun3', label: 'Double run of 3', family: 'run', count: 4, call: 'double run of 3', sig: { runLen: 3, mult: 2 } },
    { id: 'doublerun4', label: 'Double run of 4', family: 'run', count: 5, call: 'double run of 4', sig: { runLen: 4, mult: 2 } },
    { id: 'doubledoublerun3', label: 'Double double run of 3', family: 'run', count: 5, call: 'double double run of 3', sig: { runLen: 3, mult: 4 } },
    { id: 'triplerun3', label: 'Triple run of 3', family: 'run', count: 5, call: 'triple run of 3', sig: { runLen: 3, mult: 3 } },
    { id: 'flush', label: 'Flush', family: 'flush', count: [4, 5], call: 'flush' },
    { id: 'nobs', label: 'Nobs', family: 'nobs', count: [1, 2], call: 'nobs' },
];
export const kindById = (id) => KINDS.find((k) => k.id === id) || KINDS[0];

// callPhrase reads a DECLARATION aloud from its dropdown id ("double run of 3").
// atomPhrase reads a single ATOMIC component (used for missed lines): runs fold
// in their length ("run of 3"); everything else is the bare type word.
export const callPhrase = (kindId) => kindById(kindId).call;
export const atomPhrase = (a) => (a.kind === 'run' ? `run of ${a.runLen}` : a.kind);

// cardKey is an order-independent identity for a set of cards (their wire codes,
// sorted) used to match one atomic component against another.
export const cardKey = (cards) => cards.map(cardCode).sort().join(',');

// --- atomic decomposition -----------------------------------------------------
// Grading reduces every combo — engine-reported or user-declared — to the same
// alphabet of ATOMIC components: a 2-pt fifteen, a 2-pt pair (exactly 2 cards), a
// length-L / L-pt run, a 4-or-5-pt flush, a 1-pt nobs. An atom's identity is its
// type (+ run length) and its order-independent card set; matching at this level
// means a bundled "double run of 3" and its hand-decomposed pair + two runs are
// interchangeable. Each atom is { kind, cards, points, runLen? }.

// pairSubsets turns k same-rank cards into all C(k,2) atomic pairs. Used for the
// pair family (pair/pair royal/double pair royal) and for the duplicated ranks a
// run combo absorbs.
export function pairSubsets(cards) {
    const atoms = [];
    for (let i = 0; i < cards.length; i++)
        for (let j = i + 1; j < cards.length; j++)
            atoms.push({ kind: 'pair', cards: [cards[i], cards[j]], points: 2 });
    return atoms;
}

// runSignature derives the STRUCTURE a set of cards actually forms as a run:
// group by rank, the distinct ranks are the base run (its length is `runLen`),
// the product of the group sizes is the `multiplicity` (how many atomic runs the
// cartesian product yields), and `consecutive` is whether those distinct ranks
// are a single unbroken ascending sequence of at least 3. A non-run selection
// (gaps, or fewer than 3 distinct ranks) reports `consecutive:false` — it never
// throws.
export function runSignature(cards) {
    const byRank = new Map();
    for (const c of cards) byRank.set(c.rank, (byRank.get(c.rank) || 0) + 1);
    const ranks = [...byRank.keys()].sort((a, b) => a - b);
    let mult = 1;
    for (const r of ranks) mult *= byRank.get(r);
    let consecutive = ranks.length >= 3;
    for (let i = 1; i < ranks.length; i++)
        if (ranks[i] !== ranks[i - 1] + 1) consecutive = false;
    return { runLen: ranks.length, mult, consecutive };
}

// runAtoms decomposes a bundled run (single or multiple) into its atomic parts.
// Group the cards by rank: the distinct ranks form the consecutive base run, and
// the cartesian product taking one card per rank yields exactly `multiplicity`
// atomic runs of that length. Any rank appearing more than once also contributes
// its C(k,2) atomic pairs. (e.g. 3,4,5,5 → runs 3-4-5a & 3-4-5b + pair 5a-5b.)
// The SAME logic serves the engine's `run` combos and the user's run-family
// declarations; if the selected cards can't form the claimed run the generated
// atoms simply won't match the engine pool and the declaration grades wrong.
export function runAtoms(cards) {
    const byRank = new Map();
    for (const c of cards) {
        if (!byRank.has(c.rank)) byRank.set(c.rank, []);
        byRank.get(c.rank).push(c);
    }
    const ranks = [...byRank.keys()].sort((a, b) => a - b);
    const runLen = ranks.length;
    let runs = [[]];
    for (const rank of ranks) {
        const next = [];
        for (const combo of runs)
            for (const card of byRank.get(rank)) next.push([...combo, card]);
        runs = next;
    }
    const atoms = runs.map((rc) => ({ kind: 'run', runLen, cards: rc, points: runLen }));
    for (const rank of ranks) {
        const group = byRank.get(rank);
        if (group.length > 1) atoms.push(...pairSubsets(group));
    }
    return atoms;
}

// atomsFromEngineCombo expands one engine combo into atoms. fifteen/flush/nobs
// are atomic already; pair combos (pair/pair-royal/double-pair-royal, points
// k*(k-1)) fan out to their 2-card subsets; run combos go through runAtoms.
export function atomsFromEngineCombo(c) {
    const cards = (c.cards || []).map(parseCard);
    switch (c.kind) {
        case 'fifteen': return [{ kind: 'fifteen', cards, points: 2 }];
        case 'pair': return pairSubsets(cards);
        case 'run': return runAtoms(cards);
        case 'flush': return [{ kind: 'flush', cards, points: cards.length }];
        case 'nobs': return [{ kind: 'nobs', cards, points: 1 }];
        default: return [{ kind: c.kind, cards, points: c.points }];
    }
}

// atomsFromDeclaration expands a user declaration into atoms by its kind family,
// reusing the very same generators as the engine side so the two pools line up.
// For the run family the declared KIND carries a structural signature (runLen +
// multiplicity); the selected cards must actually form that structure AND a
// single consecutive base run, otherwise the declaration is mislabeled and we
// return [] so grade() marks it card-wrong (BUG4) — a plain run of 4 declared as
// "double run of 3" no longer grades green.
export function atomsFromDeclaration(entry, cards) {
    switch (entry.family) {
        case 'fifteen': return [{ kind: 'fifteen', cards, points: 2 }];
        case 'pair': return pairSubsets(cards);
        case 'run': {
            const sig = runSignature(cards);
            const want = entry.sig || {};
            if (!sig.consecutive || sig.runLen !== want.runLen || sig.mult !== want.mult) return [];
            return runAtoms(cards);
        }
        case 'flush': return [{ kind: 'flush', cards, points: cards.length }];
        case 'nobs': return [{ kind: 'nobs', cards, points: 1 }];
        default: return [];
    }
}

// atomsMatch compares a pool atom against a wanted atom by type + card set (runs
// also by length). nobs stays lenient: any engine nobs satisfies a nobs atom,
// regardless of which card(s) the user picked.
export function atomsMatch(pool, want) {
    if (want.kind === 'nobs') return pool.kind === 'nobs';
    if (pool.kind !== want.kind) return false;
    if (want.kind === 'run' && pool.runLen !== want.runLen) return false;
    return cardKey(pool.cards) === cardKey(want.cards);
}

// grade reduces the engine's combos AND the user's declarations to atomic
// components and matches them in that common alphabet, then grades each line on
// BOTH its cards and the running count the user typed. It is PURE: `declarations`
// is [{ kindId, cards:[{rank,suit}], count:int }] (the UI resolves card indices
// before calling), `rawCombos` is the engine's combo list, `engineTotal` the
// engine total. Returns { correct, total, declared:[...], missed:[...] }.
//
// Cards (cardsOk): a declaration's cards are right iff EVERY one of its atoms is
// found unconsumed in the engine pool (which it then consumes); otherwise the
// cards are wrong and the declaration consumes nothing. This is what lets a double
// run be declared bundled or fully decomposed and grade the same; nobs atoms match
// leniently. Leftover engine atoms become the "missed" list, each its own line.
//
// Running count: the expected running count at a line is the cumulative of the
// REAL points over the sequence of CORRECTLY-CARDED declarations, in order. A
// card-wrong line breaks the chain — it contributes 0 and is skipped — so a later
// correct line's expected count is the running total of the correct-carded points
// only. A line is fully correct (green ✓) iff its cards match AND the count the
// user typed equals that expected running count. Since the last correct line's
// running count is exactly the sum of all correct-carded points, "every line green
// + none missed" necessarily equals the engine total, so all-green ⇒ Correct
// always and a separate declTotal===engineTotal check is redundant.
export function grade(declarations, rawCombos, engineTotal) {
    const pool = [];
    for (const c of rawCombos)
        for (const a of atomsFromEngineCombo(c)) pool.push({ ...a, used: false });

    // First pass — cards only: decide each declaration's cardsOk and its real point
    // value, independent of the running count the user typed.
    const declared = declarations.map((d) => {
        const entry = kindById(d.kindId);
        const cards = d.cards;
        const want = atomsFromDeclaration(entry, cards);
        // Find a distinct unconsumed pool atom for each wanted atom; only commit
        // (consume) if ALL are found, so a card-wrong declaration leaves the pool intact.
        const found = [];
        let cardsOk = want.length > 0;
        for (const a of want) {
            const m = pool.find((p) => !p.used && !found.includes(p) && atomsMatch(p, a));
            if (!m) { cardsOk = false; break; }
            found.push(m);
        }
        if (cardsOk) for (const m of found) m.used = true;
        const points = cardsOk ? want.reduce((s, a) => s + a.points, 0) : 0;
        return { kindId: d.kindId, cards, count: d.count, cardsOk, points };
    });

    const missed = pool.filter((p) => !p.used)
        .map((p) => ({ kind: p.kind, cards: p.cards, points: p.points, runLen: p.runLen }));

    // Second pass — running count: walk the declarations in order, accumulating the
    // real points of correctly-carded lines only. Each correct-carded line records
    // the running count it SHOULD read (correctCount); a line is green only if the
    // count the user typed matches it. Card-wrong lines don't advance the chain and
    // carry no expected running count.
    let run = 0;
    for (const d of declared) {
        if (d.cardsOk) {
            run += d.points;
            d.correctCount = run;            // the running count this line should read
            d.correct = d.count === run;     // green only when cards AND count agree
        } else {
            d.correctCount = null;
            d.correct = false;
        }
    }
    // Missed atoms continue the same running count so the teaching progression ends
    // at the engine total.
    for (const m of missed) { run += m.points; m.correctCount = run; }

    // Overall verdict: every declared line green (cards right AND count right) and
    // nothing missed. All-green + none-missed ⇒ the last line's running count equals
    // the engine total, so "all lines ✓ yet Not quite" is impossible by construction.
    const correct = declared.every((d) => d.correct) && missed.length === 0;

    return { correct, total: engineTotal, declared, missed };
}
