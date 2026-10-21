// The landing page's one script. Its first half rotates the hero through four
// made-up records so the scene reads as a stream of taps rather than a still.
//
// THE ROTATION IS THE REFERENCE'S (docs/design/landing-reference-2026-09-12.html),
// moved from an inline <script> into this file: inline would have needed
// script-src 'unsafe-inline' in the marketing policy; a file needs script-src
// 'self', which is what internal/handler/marketing.go's landingCSP grants and
// nothing more. LP-03 (2026-10-21) widened it to the plaque and the phone the boards
// draw, so the ticket, the phone and the plaque always show ONE record.
//
// 🔴 IT MOVES NOTHING (README O2). It swaps words; it adds no class and starts no
// motion. The page is drawn still and stays still.
//
// WHAT IT TOUCHES: the eight text nodes of the record, and the scene's pause button
// -- its words and its [hidden] attribute. No fetch, no storage, no cookie, no
// location read (§4.2 — that belongs to tap.js and to the moment of a tap).
// Everything it writes is invented and is labelled as such on the page. Every
// venue is one of the seed's (test/fixtures/seed.sql), and the page's own markup
// is the FIRST record below, so a browser that runs no script shows that record
// whole -- both held by the LandingHero tests in internal/handler.
//
// A reader who has asked for less motion gets the first record and no timer, and
// so does a page that lost one of the eight nodes: a rotation that could fill only
// some of them would show two records at once.
//
// 🔴 WHAT CHANGES ON ITS OWN MUST BE STOPPABLE (WCAG 2.2.2), AND THE STOPPING IS A
// STATE MACHINE WRITTEN AS DATA. Two facts: `paused` (the reader's choice) and
// `hovered` (a mouse over the scene). PAUSE_MACHINE says what each event does to
// them, next() applies it and nothing else changes them; the rotation runs while
// both are false. The table is JSON so internal/handler can read it from this file
// and check every change it makes (TestLandingHero_ThePauseMachineIsTheTable) -- there
// is no Node here to run this file.
//   press         the button: flips `paused`, and only `paused`. One press on
//                 "Pause" stops, one press on "Play" resumes, by mouse, touch,
//                 Enter or Space alike.
//   pointerenter  a MOUSE over the scene: a quiet, temporary stop. It does not
//   pointerleave  change the button's word and a press does not clear it. Touch is
//                 not hover: a tap's compatibility mouseenter would otherwise stop
//                 the scene before the press reached the button.
//   keyfocus      KEYBOARD focus entering the scene (:focus-visible, not a click's
//                 focus): APG's auto-rotating carousel -- it stops, for good, and
//                 the button says "Play". Focus leaving does not restart it; only
//                 the button does.
// The button's word comes from `paused` alone: it always names what pressing does.
// The button ships [hidden]: with no script there is no dead control, and with no
// button there is no rotation. Under reduced motion it stays hidden: nothing changes
// on its own, so there is nothing to pause, and a "Play" would offer the motion the
// reader declined.
(function () {
  var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var people = [
    { n: 'Maria Vella', t: 'CLOCK IN', time: '09:58', loc: 'KF St Julians', plaque: 'KF ST JULIANS · 91AC-7E55' },
    { n: 'Kevin Grech', t: 'CLOCK IN', time: '09:52', loc: 'KF St Julians', plaque: 'KF ST JULIANS · 91AC-7E55' },
    { n: 'Anna Farrugia', t: 'CLOCK IN', time: '04:02', loc: 'KM Plant', plaque: 'KM PLANT · 2F04-1B9A' },
    { n: 'Luke Borg', t: 'CLOCK OUT', time: '02:04', loc: 'Rusty Bar', plaque: 'RUSTY BAR · 6D31-C8E2' }
  ];
  // id -> the record's field it shows.
  var slots = {
    dName: 'n', dType: 't', dTime: 'time', dLoc: 'loc',
    pName: 'n', pLoc: 'loc', pTime: 'time', pPlaque: 'plaque'
  };
  var PAUSE_MACHINE = {"press": {"paused": "flip"}, "pointerenter": {"hovered": true}, "pointerleave": {"hovered": false}, "keyfocus": {"paused": true}};
  function next(state, event) {
    var out = { paused: state.paused, hovered: state.hovered };
    var change = PAUSE_MACHINE[event];
    for (var k in change) out[k] = change[k] === 'flip' ? !out[k] : change[k];
    return out;
  }
  function label(state) { return state.paused ? 'Play' : 'Pause'; }
  function rotating(state) { return !state.paused && !state.hovered; }

  var nodes = {};
  for (var id in slots) {
    nodes[id] = document.getElementById(id);
    if (!nodes[id]) return;
  }
  var pause = document.getElementById('heroPause');
  var scene = pause && pause.closest('.scene');
  if (reduced || !scene) return;

  var state = { paused: false, hovered: false };
  function on(event) {
    state = next(state, event);
    pause.textContent = label(state);
  }
  function keyboardFocus(el) {
    try { return el.matches(':focus-visible'); } catch (err) { return false; }
  }
  pause.addEventListener('click', function () { on('press'); });
  scene.addEventListener('pointerenter', function (e) { if (e.pointerType === 'mouse') on('pointerenter'); });
  scene.addEventListener('pointerleave', function (e) { if (e.pointerType === 'mouse') on('pointerleave'); });
  scene.addEventListener('focusin', function (e) { if (keyboardFocus(e.target)) on('keyfocus'); });
  pause.removeAttribute('hidden');

  var i = 0;
  setInterval(function () {
    if (!rotating(state)) return;
    i = (i + 1) % people.length;
    var p = people[i];
    for (var id in slots) nodes[id].textContent = p[slots[id]];
  }, 3800);
})();


// ---------------------------------------------------------------------------
// The "Watch demo" dialog (2026-09-14). NOT part of the user's reference design
// -- the reference's second hero button scrolled to "How it works" -- so it is
// written to this repository's rules rather than copied.
//
// 🔴 EVERYTHING THAT IS HARD ABOUT A MODAL IS DONE BY THE BROWSER. showModal()
// puts the dialog in the top layer, traps Tab inside it, makes the rest of the
// page inert, closes on Escape and paints ::backdrop. That is why there is no
// library here, no focus ring to save and restore by hand, no keydown listener
// for Escape and no aria-modal attribute: asserting aria-modal on an element the
// UA has already made modal is a second source of truth.
//
// WHAT IS LEFT IS FOUR THINGS THE PLATFORM DOES NOT DO:
//   1. open it from the hero link, and cancel that link's navigation;
//   2. close it when the pointer lands on the backdrop rather than the panel;
//   3. put focus back on the link that opened it;
//   4. stop the video, so nothing keeps talking behind a closed dialog.
//
// PROGRESSIVE ENHANCEMENT IS THE REASON THE OPENER IS A LINK. Without this script
// -- or in a browser with no <dialog> -- #demoOpen is still an <a href="#how">
// that scrolls to the section explaining how it works. Nothing below runs unless
// both ends are present and showModal is a function, so a half-supported browser
// gets the fallback rather than a dead button.
//
// It reads no cookie, makes no request and stores nothing.
(function () {
  var opener = document.getElementById('demoOpen');
  var dialog = document.getElementById('demoDialog');
  if (!opener || !dialog || typeof dialog.showModal !== 'function') return;

  // The <video> is absent whenever the build carries no recording; every use of
  // it below is guarded, because the empty dialog is the state that ships today.
  var video = dialog.querySelector('video');

  // Announced only now that the behaviour exists. In the fallback the element is
  // an ordinary link and must not claim otherwise.
  opener.setAttribute('aria-haspopup', 'dialog');
  opener.setAttribute('aria-controls', 'demoDialog');

  function open(e) {
    if (e) e.preventDefault();
    if (dialog.open) return;
    dialog.showModal(); // initial focus goes to [autofocus], the close button
  }

  opener.addEventListener('click', open);

  // Space activates a <button> but not an <a>; it scrolls the page instead. Now
  // that this link behaves like a button, it answers to the button's key too.
  opener.addEventListener('keydown', function (e) {
    if (e.key === ' ' || e.key === 'Spacebar') open(e);
  });

  // A click whose target is the dialog ELEMENT itself landed on the backdrop:
  // the panel's own padding is zero and its children cover it, so nothing inside
  // reports the dialog as its target.
  dialog.addEventListener('click', function (e) {
    if (e.target === dialog) dialog.close();
  });

  dialog.querySelector('#demoClose').addEventListener('click', function () {
    dialog.close();
  });

  // ONE HANDLER FOR ALL THREE WAYS OUT. Escape, the backdrop and the button all
  // end in a `close` event, so the video is stopped and focus is returned exactly
  // once, from one place, and no exit can be added that forgets either.
  dialog.addEventListener('close', function () {
    if (video) {
      video.pause();
      video.currentTime = 0; // reopening starts at the beginning, not mid-sentence
    }
    opener.focus();
  });
})();
