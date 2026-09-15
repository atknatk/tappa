// The landing page's one script: the ticket in the hero rotates through four
// made-up records so the scene reads as a stream of taps rather than a still.
//
// 🔴 IT IS THE USER'S OWN SCRIPT, CARRIED OVER FROM
// docs/design/landing-reference-2026-09-12.html UNCHANGED apart from being a
// file instead of an inline <script>. Inline would have needed
// script-src 'unsafe-inline' in the marketing policy; a file needs
// script-src 'self', which is what internal/handler/marketing.go's landingCSP
// grants and nothing more.
//
// IT TOUCHES NOTHING BUT FOUR TEXT NODES. No fetch, no storage, no cookie, no
// location read (§4.2 — that belongs to tap.js and to the moment of a tap).
// Everything it writes is invented and is labelled as such on the page.
//
// A reader who has asked for less motion gets the first record and no timer.
(function () {
  var reduced = window.matchMedia('(prefers-reduced-motion: reduce)').matches;
  var people = [
    { n: 'Maria Vella', t: 'CLOCK IN', time: '09:58', loc: 'KF St Julians' },
    { n: 'Kevin Grech', t: 'CLOCK IN', time: '09:52', loc: 'KF St Julians' },
    { n: 'Anna Farrugia', t: 'CLOCK IN', time: '04:02', loc: 'KM Facility' },
    { n: 'Luke Borg', t: 'CLOCK OUT', time: '02:04', loc: 'Rusty Bar' }
  ];
  var i = 0;
  if (reduced) return;
  setInterval(function () {
    i = (i + 1) % people.length;
    var p = people[i];
    document.getElementById('dName').textContent = p.n;
    document.getElementById('dType').textContent = p.t;
    document.getElementById('dTime').textContent = p.time;
    document.getElementById('dLoc').textContent = p.loc;
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
