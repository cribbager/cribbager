// Dev-only harness for the board's "render from a View" path. It imports the game
// module (which, seeing window.__CRIBBAGER_NO_BOOT__, exposes its render harness
// instead of booting a live game) and drives renderFromView with hand-authored,
// server-shaped View objects. This is the same projection the multiplayer board
// uses, so what you see here is exactly what a real player would see for that View.
//
// A "View" here mirrors the server's PlayerView (internal/game/view.go): seats are
// absolute (0/1); cards are wire strings ("AS", "TD", "JH"); Phase is one of
// 'discard' | 'play' | 'complete'; Starter/Winner/ToPlay are null when absent.
import '/src/ui/main.js';

const harness = window.__cribbagerRenderTest;
const $ = (id) => document.getElementById(id);

// ---- preset Views (everything renderFromView reads) ----
const fresh6 = ['5C', '5D', '6H', '8S', 'JC', 'KD'];
const presets = {
  'Fresh deal (discard)': {
    You: 0, Dealer: 0, Phase: 'discard', Scores: [0, 0],
    YourHand: fresh6, OpponentCards: 6, CribCards: 0,
    Starter: null, Pile: [], YourPlayed: [], OpponentPlayed: [],
    Count: 0, ToPlay: null, LegalPlays: [], Winner: null, Version: 3,
  },
  'Mid-play (pile + count 15)': {
    You: 0, Dealer: 1, Phase: 'play', Scores: [12, 8],
    YourHand: ['7H', '8S', 'KD'], OpponentCards: 3, CribCards: 4,
    Starter: 'QC', Pile: ['TD', '5C'], YourPlayed: ['5C'], OpponentPlayed: ['TD'],
    Count: 15, ToPlay: 0, LegalPlays: ['7H', '8S', 'KD'], Winner: null, Version: 21,
  },
  'Post-cut, Jack starter (heels)': {
    You: 0, Dealer: 0, Phase: 'play', Scores: [2, 0],
    YourHand: ['4C', '5D', '6H', '7S'], OpponentCards: 4, CribCards: 4,
    Starter: 'JS', Pile: [], YourPlayed: [], OpponentPlayed: [],
    Count: 0, ToPlay: 1, LegalPlays: [], Winner: null, Version: 9,
  },
  'Near-skunk score': {
    You: 0, Dealer: 1, Phase: 'play', Scores: [118, 55],
    YourHand: ['9C', 'TD'], OpponentCards: 2, CribCards: 4,
    Starter: '3H', Pile: ['6S'], YourPlayed: ['7S', '8H'], OpponentPlayed: ['6S'],
    Count: 6, ToPlay: 0, LegalPlays: ['9C', 'TD'], Winner: null, Version: 88,
  },
  'Game over (121 – 90)': {
    You: 0, Dealer: 0, Phase: 'complete', Scores: [121, 90],
    YourHand: [], OpponentCards: 0, CribCards: 4,
    Starter: '3H', Pile: [], YourPlayed: ['9C', 'TD', 'JS', 'QH'], OpponentPlayed: ['6S', '6D', '7C', '8H'],
    Count: 0, ToPlay: null, LegalPlays: [], Winner: 0, Version: 142,
  },
};

function paint(view) {
  $('err').textContent = '';
  try {
    harness.setSeat(Number($('seat').value));
    harness.reset();              // clear any prior scenario (board + transient state)
    harness.renderFromView(view); // the exact path the live board uses
  } catch (e) {
    $('err').textContent = String(e && e.message ? e.message : e);
  }
}

function load(view) {
  $('json').value = JSON.stringify(view, null, 2);
  paint(view);
}

// Preset buttons.
for (const [label, view] of Object.entries(presets)) {
  const b = document.createElement('button');
  b.textContent = label;
  b.addEventListener('click', () => load(view));
  $('presets').append(b);
}

// Render whatever's in the textarea (edited preset or pasted raw View).
$('render').addEventListener('click', () => {
  let view;
  try { view = JSON.parse($('json').value); }
  catch (e) { $('err').textContent = 'Invalid JSON: ' + e.message; return; }
  paint(view);
});

// Re-render the current textarea View when the seat changes.
$('seat').addEventListener('change', () => {
  try { paint(JSON.parse($('json').value)); } catch { /* leave the error from render */ }
});

// Start on the first preset.
load(Object.values(presets)[0]);
