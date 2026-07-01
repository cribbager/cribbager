import { test } from 'node:test';
import assert from 'node:assert/strict';
import {
    grade,
    runSignature,
    runAtoms,
    atomsFromDeclaration,
    atomsFromEngineCombo,
    atomsMatch,
    kindById,
} from '../public/src/ui/scoringQuizGrading.js';
import { parseCard } from '../public/src/engine/cards.js';

// --- helpers ------------------------------------------------------------------
// Real engine combos come back as { kind, cards, points, run_length, multiplicity }
// (POST /tools/score-hand). grade() only reads kind/cards/points, so these mirror
// the wire shape while carrying the fields the client actually consumes.
const combo = (kind, cards, points, extra = {}) => ({ kind, cards, points, ...extra });
// A declaration as grade() consumes it: kindId + resolved cards + running count.
const decl = (kindId, cards, count) => ({ kindId, cards: cards.map(parseCard), count });

// callsOk asserts the per-line correctness pattern (booleans) and no missed atoms
// unless told otherwise; returns the graded result for further assertions.
function gradeDecls(decls, combos, total) {
    return grade(decls, combos, total);
}

// --- runSignature (the new run-signature rule) --------------------------------
test('runSignature: plain run of 4 is runLen 4, mult 1, consecutive', () => {
    const sig = runSignature(['3C', '4D', '5S', '6H'].map(parseCard));
    assert.deepEqual(sig, { runLen: 4, mult: 1, consecutive: true });
});

test('runSignature: double run of 3 is runLen 3, mult 2', () => {
    const sig = runSignature(['3C', '4D', '5S', '5H'].map(parseCard));
    assert.deepEqual(sig, { runLen: 3, mult: 2, consecutive: true });
});

test('runSignature: triple run of 3 is runLen 3, mult 3', () => {
    const sig = runSignature(['5C', '5D', '5S', '4H', '6C'].map(parseCard));
    assert.deepEqual(sig, { runLen: 3, mult: 3, consecutive: true });
});

test('runSignature: double double run of 3 is runLen 3, mult 4', () => {
    const sig = runSignature(['4C', '4D', '5S', '5H', '6C'].map(parseCard));
    assert.deepEqual(sig, { runLen: 3, mult: 4, consecutive: true });
});

test('runSignature: a non-consecutive selection is not a run and does not throw', () => {
    const sig = runSignature(['3C', '5D', '7S'].map(parseCard));
    assert.equal(sig.consecutive, false);
});

test('runSignature: fewer than 3 distinct ranks is not a run', () => {
    assert.equal(runSignature(['3C', '4D'].map(parseCard)).consecutive, false);
});

// --- atomsFromDeclaration signature gating ------------------------------------
test('atomsFromDeclaration: run of 4 cards declared as double run of 3 yields no atoms', () => {
    const cards = ['3C', '4D', '5S', '6H'].map(parseCard);
    assert.deepEqual(atomsFromDeclaration(kindById('doublerun3'), cards), []);
});

test('atomsFromDeclaration: run of 4 declared as run of 4 yields one run atom', () => {
    const cards = ['3C', '4D', '5S', '6H'].map(parseCard);
    const atoms = atomsFromDeclaration(kindById('run4'), cards);
    assert.equal(atoms.length, 1);
    assert.equal(atoms[0].kind, 'run');
    assert.equal(atoms[0].runLen, 4);
});

test('atomsFromDeclaration: a non-run selection labeled Run of 3 grades to no atoms (no throw)', () => {
    const cards = ['3C', '5D', '7S'].map(parseCard);
    assert.deepEqual(atomsFromDeclaration(kindById('run3'), cards), []);
});

// --- double run of 3: bundled vs decomposed both grade correct ----------------
// The engine bundles a double run into ONE run combo carrying multiplicity, which
// atomsFromEngineCombo expands via runAtoms into 2 runs + 1 pair.
test('double run of 3: engine bundles one run combo (mult 2) into 2 runs + 1 pair atoms', () => {
    const c = combo('run', ['3C', '4D', '5S', '5H'], 8, { run_length: 3, multiplicity: 2 });
    const atoms = atomsFromEngineCombo(c);
    const runs = atoms.filter((a) => a.kind === 'run');
    const pairs = atoms.filter((a) => a.kind === 'pair');
    assert.equal(runs.length, 2);
    assert.equal(pairs.length, 1);
    assert.equal(runs.reduce((s, a) => s + a.points, 0) + pairs.reduce((s, a) => s + a.points, 0), 8);
});

test('double run of 3 graded correct as ONE bundled "Double run of 3" declaration', () => {
    const combos = [combo('run', ['3C', '4D', '5S', '5H'], 8, { run_length: 3, multiplicity: 2 })];
    const r = gradeDecls([decl('doublerun3', ['3C', '4D', '5S', '5H'], 8)], combos, 8);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].correct, true);
    assert.equal(r.missed.length, 0);
});

test('double run of 3 graded correct as the DECOMPOSED pair + two runs of 3 (atomic equivalence)', () => {
    const combos = [combo('run', ['3C', '4D', '5S', '5H'], 8, { run_length: 3, multiplicity: 2 })];
    const decls = [
        decl('pair', ['5S', '5H'], 2),
        decl('run3', ['3C', '4D', '5S'], 5),
        decl('run3', ['3C', '4D', '5H'], 8),
    ];
    const r = gradeDecls(decls, combos, 8);
    assert.equal(r.correct, true);
    assert.deepEqual(r.declared.map((d) => d.correct), [true, true, true]);
    assert.equal(r.missed.length, 0);
});

// --- BUG4 regression ----------------------------------------------------------
test('BUG4: a plain run of 4 declared as "Double run of 3" grades WRONG (not green)', () => {
    // Hand 3C 4D 5S 6H + KC: a single run of 4 (4 pts), nothing else.
    const combos = [combo('run', ['3C', '4D', '5S', '6H'], 4, { run_length: 4, multiplicity: 1 })];
    const r = gradeDecls([decl('doublerun3', ['3C', '4D', '5S', '6H'], 4)], combos, 4);
    assert.equal(r.declared[0].correct, false);
    assert.equal(r.declared[0].cardsOk, false); // mislabeled: cards don't form a double run of 3
    assert.equal(r.correct, false);
    // The real run-of-4 atom is left unconsumed → surfaces as a missed line.
    assert.equal(r.missed.length, 1);
    assert.equal(r.missed[0].kind, 'run');
});

test('BUG4: the same run of 4 declared correctly as "Run of 4" grades green', () => {
    const combos = [combo('run', ['3C', '4D', '5S', '6H'], 4, { run_length: 4, multiplicity: 1 })];
    const r = gradeDecls([decl('run4', ['3C', '4D', '5S', '6H'], 4)], combos, 4);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].correct, true);
});

test('BUG4 symmetric: a triple run of 3 declared as "Run of 5" grades WRONG', () => {
    // 5C 5D 5S 4H 6C: triple run of 3 = run 3*3 = 9 + three pairs of 5s = 6 → 15.
    const combos = [combo('run', ['5C', '5D', '5S', '4H', '6C'], 15, { run_length: 3, multiplicity: 3 })];
    const r = gradeDecls([decl('run5', ['5C', '5D', '5S', '4H', '6C'], 15)], combos, 15);
    assert.equal(r.declared[0].correct, false);
    assert.equal(r.correct, false);
});

// --- triple run of 3 & double double run of 3: correct labels → correct -------
test('triple run of 3 declared correctly grades green', () => {
    const combos = [combo('run', ['5C', '5D', '5S', '4H', '6C'], 15, { run_length: 3, multiplicity: 3 })];
    const r = gradeDecls([decl('triplerun3', ['5C', '5D', '5S', '4H', '6C'], 15)], combos, 15);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].correct, true);
    assert.equal(r.missed.length, 0);
});

test('double double run of 3 declared correctly grades green', () => {
    // 4C 4D 5S 5H 6C: base run 4-5-6, mult 4 → run 3*4 = 12 + two pairs = 4 → 16.
    const combos = [combo('run', ['4C', '4D', '5S', '5H', '6C'], 16, { run_length: 3, multiplicity: 4 })];
    const r = gradeDecls([decl('doubledoublerun3', ['4C', '4D', '5S', '5H', '6C'], 16)], combos, 16);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].correct, true);
    assert.equal(r.missed.length, 0);
});

// --- flush: 4-card hand flush vs 5-card flush ---------------------------------
test('4-card flush declared as flush grades green', () => {
    const combos = [combo('flush', ['2C', '5C', '8C', 'JC'], 4)];
    const r = gradeDecls([decl('flush', ['2C', '5C', '8C', 'JC'], 4)], combos, 4);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].points, 4);
});

test('5-card flush declared as flush grades green with 5 points', () => {
    const combos = [combo('flush', ['2C', '5C', '8C', 'JC', 'KC'], 5)];
    const r = gradeDecls([decl('flush', ['2C', '5C', '8C', 'JC', 'KC'], 5)], combos, 5);
    assert.equal(r.correct, true);
    assert.equal(r.declared[0].points, 5);
});

// --- nobs leniency ------------------------------------------------------------
test('nobs accepted when declared as the Jack alone', () => {
    const combos = [combo('nobs', ['JH'], 1)];
    const r = gradeDecls([decl('nobs', ['JH'], 1)], combos, 1);
    assert.equal(r.correct, true);
});

test('nobs accepted when declared as Jack + starter', () => {
    const combos = [combo('nobs', ['JH'], 1)];
    const r = gradeDecls([decl('nobs', ['JH', '4H'], 1)], combos, 1);
    assert.equal(r.correct, true);
});

test('nobs accepted (leniently) when declared as the starter alone', () => {
    const combos = [combo('nobs', ['JH'], 1)];
    const r = gradeDecls([decl('nobs', ['4H'], 1)], combos, 1);
    assert.equal(r.correct, true);
});

test('nobs fails when the hand has no nobs (no fake point)', () => {
    const r = gradeDecls([decl('nobs', ['JH'], 1)], [], 0);
    assert.equal(r.declared[0].cardsOk, false);
    assert.equal(r.correct, false);
});

// --- 0-point hand -------------------------------------------------------------
test('a 0-point hand with no declarations grades Correct', () => {
    const r = gradeDecls([], [], 0);
    assert.equal(r.correct, true);
    assert.equal(r.declared.length, 0);
    assert.equal(r.missed.length, 0);
});

// --- invariants ---------------------------------------------------------------
test('all-green declared lines with nothing missed ⇒ overall Correct', () => {
    // 5H 5S 5C JD + 5D: fifteen? none (5+5+5=15 is a fifteen of three fives).
    // Use a clean multi-combo hand: 4C 5D 6S 7H + 8C → run of 5 (5) only.
    const combos = [combo('run', ['4C', '5D', '6S', '7H', '8C'], 5, { run_length: 5, multiplicity: 1 })];
    const r = gradeDecls([decl('run5', ['4C', '5D', '6S', '7H', '8C'], 5)], combos, 5);
    assert.equal(r.declared.every((d) => d.correct) && r.missed.length === 0, true);
    assert.equal(r.correct, true);
});

test('right cards but wrong running count reads wrong and shows the correct "should be" count', () => {
    const combos = [combo('run', ['4C', '5D', '6S'], 3, { run_length: 3, multiplicity: 1 })];
    // Cards are a real run of 3 (worth 3) but the user typed 7 as the running count.
    const r = gradeDecls([decl('run3', ['4C', '5D', '6S'], 7)], combos, 3);
    assert.equal(r.declared[0].cardsOk, true);      // cards are right
    assert.equal(r.declared[0].correct, false);     // but the count is wrong
    assert.equal(r.declared[0].correctCount, 3);    // the count it SHOULD read
    assert.equal(r.correct, false);
});

// --- atomsMatch / runAtoms sanity --------------------------------------------
test('atomsMatch: nobs is lenient, other kinds match by card set', () => {
    const jh = { kind: 'nobs', cards: [parseCard('JH')] };
    const jd = { kind: 'nobs', cards: [parseCard('JD')] };
    assert.equal(atomsMatch(jh, jd), true); // nobs ignores which card
    const runA = { kind: 'run', runLen: 3, cards: ['3C', '4D', '5S'].map(parseCard) };
    const runB = { kind: 'run', runLen: 3, cards: ['3C', '4D', '5H'].map(parseCard) };
    assert.equal(atomsMatch(runA, runB), false); // different card set
});

test('runAtoms: double run of 3 expands to 2 runs + 1 pair summing to 8', () => {
    const atoms = runAtoms(['3C', '4D', '5S', '5H'].map(parseCard));
    assert.equal(atoms.filter((a) => a.kind === 'run').length, 2);
    assert.equal(atoms.filter((a) => a.kind === 'pair').length, 1);
    assert.equal(atoms.reduce((s, a) => s + a.points, 0), 8);
});
