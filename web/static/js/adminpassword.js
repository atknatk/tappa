// The sign-in screen's one script: the reveal button on the password field.
//
// 🔴 IT IS THE ONLY SCRIPT ON THIS SURFACE, AND THE POLICY THAT PERMITS IT IS
// NARROWER THAN THE PANEL'S. internal/handler/adminlogin.go answers /admin/login --
// and only /admin/login, on GET and on the 401 re-render -- with adminLoginCSP,
// which is adminCSP plus script-src 'self'. Every other panel screen keeps the
// unwidened adminCSP and would refuse this file. There is no 'unsafe-inline' and no
// 'unsafe-eval' anywhere in either: this is a FILE, embedded in the binary by
// web/embed.go and served from our own origin, which is the whole of the widening.
//
// WHAT IT TOUCHES: the `type` attribute of one input, and the visibility of one
// button and two icons. NOTHING ELSE. No fetch, no storage, no cookie, no location
// read, and it never reads the field's VALUE -- the secret is only ever in the
// input the person typed it into.
//
// 🔴 THE BUTTON SHIPS [hidden] AND THIS FILE IS WHAT REVEALS IT. That is deliberate
// and it is the no-JavaScript contract: if this file does not run -- blocked,
// failed, or refused by a policy -- the person sees no control at all and the form
// works exactly as it did before the button existed. The alternative (a visible
// button that a script later wires up) would paint a dead control on the one screen
// where somebody is already unsure whether they typed their password correctly.
//
// THE FIELD RETURNS TO type="password" THE MOMENT IT IS EMPTIED. Clearing a
// revealed field and walking away must not leave an input that will show the next
// thing typed into it in the clear.
(function () {
  'use strict';

  var field = document.getElementById('admin-password');
  var button = document.querySelector('[data-pw-toggle]');
  if (!field || !button) return;

  var iconShow = button.querySelector('[data-pw-icon="show"]');
  var iconHide = button.querySelector('[data-pw-icon="hide"]');
  if (!iconShow || !iconHide) return;

  // 🔴 setHidden WRITES THE ATTRIBUTE, AND THE FIRST VERSION OF THIS FILE SET THE
  // .hidden PROPERTY INSTEAD. That works on the button and SILENTLY DOES NOTHING ON
  // THE ICONS: `hidden` is an IDL attribute of HTMLElement, and an <svg> is an
  // SVGElement, which does not inherit it. `svg.hidden = true` therefore creates a
  // plain JavaScript property, reflects nothing into the DOM, and matches no
  // [hidden] rule.
  //
  // IT WAS MEASURED, NOT REASONED ABOUT, and it is worth saying how — because every
  // cheap check passed. Reading el.hidden back returned the value just written (the
  // expando), so an assertion on the property was green in both states. What caught
  // it was a SCREENSHOT: the revealed state and the concealed state rendered the
  // same eye, and asking the page which icon actually had display:none answered
  // "neither" twice. The attribute is what the stylesheet reads, so the attribute is
  // what this writes.
  function setHidden(el, on) {
    if (on) {
      el.setAttribute('hidden', '');
    } else {
      el.removeAttribute('hidden');
    }
  }

  // reveal() writes all four things that describe one state, so there is no way to
  // change the type without changing what the button is called. An icon that says
  // "hide" beside a label that says "Show password" is worse than no icon.
  function reveal(on) {
    field.type = on ? 'text' : 'password';
    button.setAttribute('aria-pressed', on ? 'true' : 'false');
    // The name says what the NEXT press does, which is what a person reaching for
    // it needs; aria-pressed carries the current state for anyone who wants it.
    button.setAttribute('aria-label', on ? 'Hide password' : 'Show password');
    setHidden(iconShow, on);
    setHidden(iconHide, !on);
  }

  // sync() is called on every keystroke rather than only on the first, because the
  // interesting edge is the field going EMPTY again: the button goes away and the
  // field goes back to password in the same step.
  function sync() {
    var typed = field.value.length > 0;
    setHidden(button, !typed);
    if (!typed) reveal(false);
  }

  // 🔴 THE PRESS DOES NOT MOVE FOCUS, AND THE FIRST VERSION OF THIS FILE DID.
  // It called field.focus() on the theory that putting the caret back where the
  // person was typing is friendlier than leaving it on the button. MEASURED IN A
  // REAL BROWSER, that theory breaks the keyboard: pressing Space activated the
  // button, focus jumped to the input, and the NEXT key went to the input instead
  // — so a keyboard user could not press the control twice, and their Enter
  // submitted the form. Leaving focus where the person put it is both the
  // accessible answer and the one with no surprise in it.
  button.addEventListener('click', function () {
    reveal(field.type === 'password');
  });

  // 'input' rather than 'keyup': it fires for a paste, for a password manager fill
  // and for a drag-and-drop, none of which are keystrokes.
  field.addEventListener('input', sync);

  // The script is deferred, so the document is parsed by now and a value may
  // already be in the field (a manager filled it, or the browser restored it on a
  // back navigation). Starting from the field's actual state rather than from
  // "empty" is what keeps the button's presence and the field's contents agreeing.
  sync();
})();
