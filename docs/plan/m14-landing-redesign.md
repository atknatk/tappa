# M14 — Landing yeniden tasarımı: düzelt + uyum (kapsam B)

> **Durum:** plan (2026-10-21). Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya
> "yapıldı" yazılmaz.
>
> **Teslim paketi (okumadan başlama):** [docs/design/landing-redesign/README.md](../design/landing-redesign/README.md)
> — kararlar LP-0…LP-5 ve sonraki iki kullanıcı kararı (R-1 landing.js dönüşü, R-2 FAQ API metni ayrı
> dalda), kısıtlar (CSP, satır içi stil yok, tek `<nav>` önce, tek `<h1>`), sabitleyen testlerin tam
> listesi (§4), `.lp` token haritası (§5), bölüm bölüm ekran haritası ve **NEW CLAIM** tablosu (§6),
> paylaşılan pazarlama başlığı (§7), port notları ve ikon yolları (§8). `file:line` atıfları HEAD
> `945b6b9`'a göredir. Panolar: `docs/design/panel-redesign/boards/Landing.dc.html`, `T-Landing`,
> `M-Landing`, `LandingDemo`, `M-LandingDemo` (+ yeniden başlıklanan `Signup`/`Legal` × 3 genişlik).

## Kullanıcı kararları (2026-10-21) — bağlayıcı

- **LP-0 — Kapsam B: "Düzelt + uyum".** 2026-09-14'ün *"birebir"* kararı yumuşatıldı, kaldırılmadı:
  referansın yapısı, sırası, sesi ve her iddiası kalır; kusurlar düzeltilir, görünüm yeniden tasarlanan
  panel/pazarlama kabuğuna hizalanır, panel vitrini ve çalışanın telefon ekranı eklenir. Sıfırdan
  tasarım değildir.
- **LP-1 — Metin (D1 "kod gerçektir").** İddia taşıyan her metin `landingview.go`'da testle sabit bir
  değerdir; panolar onları kelimesi kelimesine kullanır. Yeni metin yalnız panel vitrininde ve telefon
  ekranında; her biri **NEW CLAIM** → `landingview.go` değeri + Fact + türetme + mutasyon kontrolü.
  Uydurma özellik, türetilmemiş sayı, referans/müşteri logosu, "Book a pilot" yok.
- **LP-2 — Kusurlar:** telefonda "Sign in" görünür; hero'da hiçbir şey plaketin yazısını, damga hiçbir
  docket satırını örtmez; karşılaştırma tablosu her genişlikte iki sütunu da taşır; emoji yok (çizgi
  ikonlar; altbilgi "© 2026 Taptime · Malta"); IBM Plex Mono yalnız 400/700.
- **LP-3 — Hizalama:** tek pazarlama başlığı (yeşil halka işareti + taptime + PUNCHLESS, `/`'a gider;
  landing'in çapaları + Sign in + Start free); D2 köşeleri; yalnız palet token'ları (+ `--mint`);
  masaüstünde FAQ iki sütun, gizlilik notu ızgara boyu.
- **LP-4 — Eklemeler:** hero'da çalışanın telefonu (tap ekranı `tap.templ`'deki haliyle, **değişmeden**)
  ve basılan docket; Security'den sonra yeni "The panel" bölümü (The Pass, The Spike, The Roll
  minyatürleri, HTML/CSS, örnek veri uydurma ve öyle etiketli); demo diyaloğu iki durumuyla.
- **LP-5 — Kısıtlar:** CSP `style-src 'self'`, `img-src` yok (yalnız satır içi SVG), betik yalnız
  `landing.js`, yeni JS dosyası yok, üçüncü taraf istek yok, fontlar kendi sunucumuzda.
- **R-1 — landing.js dönüşü plaket satırını da kapsar** (fix turu kararı). Mevcut 3,8 sn aralığında
  **üç ek metin yazımı**: `pName`, `pLoc`, `pPlaque`. Aynı dosya; başka yeni JS yok.
- **R-2 — FAQ API metni ayrı sevk edilir:** dal **`claude/fix-landing-faq-api`** (`1054fea`; bu dalda ve
  `main`'de henüz yok). Q4/Q5'i yeniden yazar, `FactNoIntegrationAPI` tuzağını emekliye ayırır, üç API
  Fact'i ve "`/v1` bağlıyken 'no API' diyen sayfa" tarayıcı testini ekler. M14 onu yeniden yazmaz;
  **M14-10 ona bağlıdır** (aşağıda).

**Yeni okuma, yeni rota, yeni migration yok.** Landing statik bir sayfadır; vitrinin iddiaları mevcut
rotalara ve karar motoruna dayanır. Bir öğe yeni bir ürün özelliği isterse bu bir **karar**dır, kart
değil.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı** bir
  denetçi kabul kriterlerini tek tek doğrular. Bulgu → düzeltme → **yeni** denetçi; onay gelmeden kart
  `done` olmaz, sonrakine geçilmez. İddia ekleyen/değiştiren kartlarda (M14-08, M14-10) denetçi her
  Fact'in türetmesini ve mutasyon kontrolünü kendisi bozup kırmızıyı görür. Her UI işi skill
  `tappa-brand` okunarak.
- **Testler:** tur içinde yalnız **değişen** testler `-run` ile (ör. `go test ./internal/handler -run
  'Landing|Marketing|APIGuidePages|Templates_CarryNoInlineStyle'`) + saniyelik kapılar (`gofmt`, build,
  `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...` ve `make check` bir kartta **bir kez**,
  commit'ten önceki son doğrulamada. Yapıcı ve denetçi brief'ine bu yazılır.
- **`make gen` + `make css`** her `.templ` / `input.css` değişikliğinden sonra; üretilen dosyalar commit
  edilir.
- **Sabitlenmiş işaretleme** (README §4) değişirse test **aynı commit'te**, iddianın niyeti korunarak
  güncellenir; hiçbir iddia gevşetilmez. Henüz var olmayan bir testin adı belgeye yazılmaz
  (`TestEveryNamedTestExists`) — yeni testi tarif et, adını yapıcı koyar.
- **Golden politikası:** `TestUnbrandedScreens_AreByteIdenticalToTheGolden` ve
  `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` M14 boyunca **bayt-aynı** kalır (`layout.*`,
  `components.*`, tap/aktivasyon ekranları değişmez). Kırmızıya dönerlerse kart paylaşılan bir parçaya
  dokunmuştur; düzeltilecek olan koddur, golden değil. Değişmesi **beklenen** tek golden marka CSS
  golden'ındaki `.lp .plaque .p-brand` satırıdır (M14-01) ve marketingChrome'un bayt ölçümü (M14-02,
  golden dosyası yok; yorum + commit mesajı).
- **Çalışan ekranları kutsal** (CLAUDE.md §9): tap/sonuç/aktivasyon ekranlarına dokunulmaz; landing tap
  ekranının **kopyasını** çizer.
- **Dal:** `claude/admiring-brown-jw8hf1` (ya da kullanıcının açtığı feature dalı). `main`'e birleştirme
  bir deploy kararıdır — kullanıcı istemedikçe yok.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu.

## Her kartın miras aldığı kısıtlar (özet — tam metin README §3)

CSP değişmez (`landingCSP`; medya yalnız demo videosu derlemedeyken) · satır içi stil ve şablonda hex yok
(`TestLanding_UsesOnlyPaletteTokens`, `TestTemplates_CarryNoInlineStyle`) — her pano stili `.lp` sınıfı,
veri güdümlü geometri sabit sınıf kovası · yeni JS yok (R-1'in üç yazımı hariç; yeni düğümler koşulsuz) ·
IBM Plex Mono 400/700 · tek `<h1>`; telefonun "Hello {name}"'i `<p>`/`<span>`, `pages.TapHeading` asla ·
**ilk `<nav>`** üç çapa + Sign in + Start free'yi taşır (`TestLandingNav_EveryLinkPointsAtASectionOnThePage`),
önüne ikinci nav yok · ölçülmemiş sayı yok (`TestLanding_MakesNoUnmeasuredNumericClaim`: `%`, 99/100,
uptime, SLA, guarantee) — örnek verilerde de · iç ad yok (`TestLanding_ShowsNoUserFacingInternalName`) ·
yeni `.templ` dosyası `panelsource_test.go`'nun `nonPanelTemplates`'ine "marketing" gerekçesiyle girer ·
landing panel bileşen sınıflarını (`.docket`, `.rail-item`, `.stamp` …) yeniden kullanmaz, kuralları `.lp`
altında · palet token'ları + opaklık (README §5) · dokunma hedefi ≥ 44 px · durum asla yalnız renk değil.

## Görev listesi

| ID | Faz | Görev | Ajan | Bağımlılık |
|---|---|---|---|---|
| M14-01 | L0 | `.lp` temeli: palet dışı renklerin emekliye ayrılması, mono 500/600 → 400/700, marka golden'ının `.p-brand` satırı | yapıcı (`tappa-brand`) | — |
| M14-02 | L0 | Paylaşılan pazarlama başlığı kilidi: halka işareti marketingChrome'a (signup, legal, API rehberi) ve landing başlığına | yapıcı (`tappa-brand`) | M14-01 |
| M14-03 | L0 | Telefonda "Sign in" (LP-2.1) | yapıcı (`tappa-brand`) | M14-02 |
| M14-04 | L1 | Hero sahnesi: telefon + raydan sarkan docket + damga üst satırda + landing.js'e üç hedef (R-1) | yapıcı (`tappa-brand`) | M14-01 |
| M14-05 | L1 | How it works: emoji → çizgi ikon (`Step.Icon` → ikon anahtarı) | yapıcı (`tappa-brand`) | M14-01 |
| M14-06 | L1 | Karşılaştırma: telefonda iki sütun da (yığılmış satırlar + gösterge) | yapıcı (`tappa-brand`) | M14-01 |
| M14-07 | L1 | Security: docket kartları + ızgara boyu gizlilik notu + kalkan ikonu | yapıcı (`tappa-brand`) | M14-01 |
| M14-08 | L2 | YENİ "The panel" vitrini: üç minyatür + NEW CLAIM değerleri, Fact'leri, testleri | yapıcı (`tappa-brand`) + ayrı iddia denetimi | M14-01, M14-07 |
| M14-09 | L1 | Fits + pricing görünümü: yuvarlak çip, kurucu teklif satırı, ✓ → ikon | yapıcı (`tappa-brand`) | M14-01 |
| M14-10 | L1 | FAQ iki sütun (+ R-2 bağımlılığı) | yapıcı (`tappa-brand`) | M14-01, **FAQ-API düzeltmesi birleşmiş** |
| M14-11 | L1 | CTA + altbilgi: bayrak emojisi yok, halka işareti, Legal kartı bağlantı stili | yapıcı (`tappa-brand`) | M14-02 |
| M14-12 | L1 | Demo diyaloğu yeniden stili (`soon` / `video`) | yapıcı (`tappa-brand`) | M14-01 |
| M14-13 | L3 | Faz sonu: tam `make check` + `make audit`, güvenlik denetimi, golden kontrolü, 390/834/1440 ekran turu | denetçi + güvenlik | hepsi |

L1 kartları birbirinden bağımsızdır; her biri kendi bağımlılıkları bitince tek başına sevk edilebilir
(dal üzerinde, CI yeşil). Her kart üç genişliği **birlikte**, tek DOM ile taşır.

---

## L0 — Temel ve başlık

## M14-01 — `.lp` temeli: token'lar ve mono ağırlıkları

- **Bağımlılık:** — · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): keep the landing block on palette tokens and two mono weights`
- **Spec:** [README §5](../design/landing-redesign/README.md)

**Amaç.** Görünür düzeni değiştirmeden `.lp` bloğunu palete indirmek: README §5 tablosundaki palet dışı
17 değer (`--ink-soft`, `--green-deep`, `#F4F6F1`, `#9AA79C`, `#D98B7B`, `#9DB3A5`, `#B9C8BC`, `#33473B`,
`#24352B`, `#E2DFC9`, `#C9C4A8`, `#4B5C50`, `#CFE6D8`, `#6FA88B`, `#7A9484`, `#F2F5EF`, `#fff`) token +
opaklık olur; IBM Plex Mono'nun 500/600 kullanımları (heroCreed, eyebrow, step `.n`, link `.proof`, fit
`.k`, `d-*`, ledger `th`, strip `b`) 400 ya da 700 olur.

**Dosyalar.** `web/static/css/input.css` (`.lp` bloğu), `internal/brand/theme_test.go` (`:955` occurrence
golden'ı), derlenen `app.css`.

**Kabul kriterleri.**
- `.lp` bloğunda palet token'ları (`--ink`, `--green`, `--green-lite`, `--porcelain`, `--paper`,
  `--saffron`, `--saffron-lite`, `--tomato`, `--line`, `--mint`) dışında hex yok (grep ile gösterilir;
  token tanımlarının kendisi hariç).
- `.lp` içinde mono için `font-weight: 500|600` yok.
- `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`: `.p-brand` kuralının yeni değeri golden'da **aynı
  commit'te**; satırın kendisi ya kalır (yeni renk) ya da sınıf yeniden adlandırılırsa golden'dan çıkar —
  testin gözü açık kalır (başka bir `brand` adlı kural eklenince kırmızı).
- Kontrast: küçük metin paper'da en az ink/.70, ink zeminde en az paper/.62; "STEP n" safran değil.
- README §4'teki landing testleri yeşil (değişen yalnız CSS).

## M14-02 — Paylaşılan pazarlama başlığı kilidi (halka işareti)

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `feat(marketing): give every public page the ring lockup`
- **Spec:** [README §6.1, §7](../design/landing-redesign/README.md) · **Panolar:** `Landing` (başlık),
  `Signup`, `T-Signup`, `M-Signup`, `Legal`, `T-Legal`, `M-Legal`

**Amaç.** Her pazarlama sayfasında tek kilit: halka 28 (tappa-green disk, 6 px içeride 2 px paper halka,
opaklık .92, `aria-hidden`) + "taptime" 24 + PUNCHLESS 11 mono, ortalanmış; telefonda halka 26 +
taptime/PUNCHLESS alt alta; bütün kilit `/`'a giden ≥ 44 px bağlantı.

**Kapsam.**
- `marketingChromeWith` (`landing.templ:635`): kilit bağlantısına halka `<span aria-hidden="true">`'ı
  (Tailwind sınıfları satır içi yazılı; `.lp` sınıfı değil), `items-baseline` → `items-center`.
- `landingNav` (`landing.templ:110`): aynı kilit `.lp` sınıflarıyla; `href="#"` → `href="/"`; PUNCHLESS
  etiketi eklenir.
- `marketingChrome`'un yorumu (`landing.templ:580–585`, "M12-46 measured … byte for byte") yeni ölçümle
  yeniden yazılır: rehberin iki sayfası (API host'lu/host'suz), signup ve legal'in gövdesi aynı, yalnız
  kilidin baytları eklendi — ölçüm commit mesajında yazılı (`0bb5085` yöntemi).
- `layout.*`'a dokunulmaz.

**Dosyalar.** `web/templates/pages/landing.templ`, `web/static/css/input.css`, üretilen `*_templ.go`,
gerekirse `internal/handler/apiguidepage_test.go` / `signup_test.go` (yalnız kilit baytını okuyan bir
iddia varsa, niyet korunarak).

**Kabul kriterleri.**
- Landing, signup (her adım), dört legal sayfası ve API rehberinin iki dili başlıkta tam **bir** halka
  işareti (`aria-hidden="true"`) ve `/`'a giden kilit taşır. **Yeni test** (adını yapıcı koyar): bu
  sayfaların her birinde kilidi arar; KONTROL: halkasız eski işaretlemede kırmızı.
- `TestLandingNav_EveryLinkPointsAtASectionOnThePage` yeşil (ilk `<nav>` hâlâ üç çapa + iki bağlantı).
- `TestAPIGuidePages_AreTheGuideInTheMarketingChrome`, `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`,
  `TestAPIGuidePages_FitAPhone`, `TestSignupSurface_EverySignInLinkGoesToTheSignInPage`,
  `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce`,
  `TestTemplates_CarryNoInlineStyle`, `TestLanding_UsesOnlyPaletteTokens` yeşil.
- `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` ve
  `TestUnbrandedScreens_AreByteIdenticalToTheGolden` **bayt-aynı**.

## M14-03 — Telefonda "Sign in" (LP-2.1)

- **Bağımlılık:** M14-02 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(landing): keep Sign in in the header on a phone`
- **Spec:** [README §6.1, §8.1](../design/landing-redesign/README.md) · **Panolar:** `M-Landing` (başlık)

**Amaç.** Bugün `@media (max-width: 720px) { .lp .navLinks a:not(.lp-btn) { display: none; } }` Sign in'i
de gizliyor. Yalnız üç çapa gizlenir; Sign in ve Start free her genişlikte görünür, tek satırda.

**Dosyalar.** `web/templates/pages/landing.templ` (çapalara ayırt edici sınıf), `web/static/css/input.css`.

**Kabul kriterleri.**
- 390 px'de başlık: kilit + Sign in + Start free, taşma yok, hedefler ≥ 44 px (ekran görüntüsü).
- Tek `<nav>`, ikinci nav yok; `TestLandingNav_EveryLinkPointsAtASectionOnThePage` yeşil.
- **Yeni test** (adını yapıcı koyar): derlenmiş CSS'te 720 px kuralı Sign in bağlantısını seçmez (yalnız
  çapa sınıfını gizler); KONTROL: eski `a:not(.lp-btn)` kuralı geri konunca kırmızı.

---

## L1 — Bölümler

## M14-04 — Hero sahnesi: telefon, raydan sarkan docket, dönüş (R-1)

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** §9 (tap ekranı **çizilir,
  değiştirilmez**)
- **Commit:** `feat(landing): draw the employee's phone and hang the ticket under the plaque`
- **Spec:** [README §6.2, §8](../design/landing-redesign/README.md) · **Panolar:** `Landing`, `T-Landing`,
  `M-Landing` (`ticket` = maria | kevin | anna | luke)

**Kapsam.**
- Sahne: [plaket + altında ink bilet rayından sarkan docket] · NFC yayları (`aria-hidden`) · telefon.
  Flex; **mutlak konum yok**, hiçbir genişlikte örtüşme yok (LP-2.2). Tablet: metin üstte; telefon: ray
  plaketin altında.
- Docket: keskin köşe, delikli alt kenar; APPROVED damgası üst satırda CLOCK IN/OUT etiketinin yanında
  (bugünkü `.d-stamp` mutlak konumu kalkar); satırlar sarar; ad kendi satırında. `dName`, `dType`,
  `dTime`, `dLoc` kimlikleri **kalır**.
- Telefon: `tap.templ`'in landing'e özel kopyası — durum çubuğu, "taptime" + "punchless", docket
  ("TAPPING" / "Hello {name}" / mekân), tek "Tap" (64 px, rounded-sm, yeşil, text-xl 400) — `<span>`,
  `<button>` değil. 375 genişlikte gerçek CSS ölçüleri + sabit ölçek sınıfı (.61 / .58 / .384). Kap
  `role="img"` + genel ad ("The employee's own phone showing the tap screen"). Selamlama `<p>`/`<span>`;
  `pages.TapHeading` kullanılmaz.
- Plaket: venue · uid satırı `id="pPlaque"`; telefonda `id="pName"`, `id="pLoc"`. Üçü koşulsuz.
- `landing.js`: her kayda `plaque` alanı (README §6.2 tablosu: KF ST JULIANS · 91AC-7E55, KM FACILITY ·
  2F04-1B9A, RUSTY BAR · 6D31-C8E2); aynı 3,8 sn aralığında üç ek `textContent` yazımı. Başka değişiklik
  yok; demo yarısına dokunulmaz.
- `landingScene` yorumu (`landing.templ:184–195`, "a drawing, not a record") yeni iki uid'i ve telefonu
  kapsar. Animasyon: `tapring`/`printout` yalnız `no-preference` içinde; print-out docket'i raydan
  **aşağı** açar.

**Dosyalar.** `web/templates/pages/landing.templ`, `web/static/js/landing.js`, `web/static/css/input.css`,
`internal/handler/marketing_test.go` ya da `marketingdemo_test.go` (yeni test).

**Kabul kriterleri.**
- **Yeni test** (adını yapıcı koyar): `landing.js`'in `getElementById('…')` ile yazdığı her kimlik
  (`dName`, `dType`, `dTime`, `dLoc`, `pName`, `pLoc`, `pPlaque`, `demoOpen`, `demoDialog`, `demoClose`)
  render edilen sayfada **tam bir kez** var; KONTROL: şablondan `pPlaque` silinince kırmızı. Bugün dönüşün
  hedeflerini sabitleyen test yok.
- Sayfada tek `<h1>`; telefonun içinde başlık öğesi yok; `role="img"` + `aria-label` telefonda.
- `TestLanding_CarriesTheSlogan`, `TestLanding_ReducedMotionLeavesTheDocketVisible`,
  `TestLandingDemo_TheScriptDegradesInsteadOfBreaking`, `TestLanding_MakesNoUnmeasuredNumericClaim`,
  `TestLanding_UsesOnlyPaletteTokens`, `TestTemplates_CarryNoInlineStyle`,
  `TestMarketing_TheSameBytesForEveryVisitor` yeşil.
- `TestUnbrandedScreens_AreByteIdenticalToTheGolden` bayt-aynı (`tap.templ` değişmedi; `git diff` boş).
- 390 / 834 / 1440'ta dört kaydın her biriyle (betik elle ilerletilerek) ekran görüntüsü: plaketin "Tap
  here to clock in / out"'u ve "On site  IP + GPS ✓" okunur.

## M14-05 — How it works: çizgi ikonlar

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `feat(landing): replace the step emoji with line icons`
- **Spec:** [README §6.4, §8.3](../design/landing-redesign/README.md)

`Step.Icon string` (`landingview.go:474`, emoji) → ikon anahtarı (plaque / invite / tap gibi tipli bir
değer); SVG `landing.templ`'de anahtar üzerinde `switch` ile (README §8.3 yolları), 44 px green-lite
karo. "STEP n" ink/.70. Tablette kartlar satır olur (860).

**Kabul kriterleri.** Sayfada emoji yok (**yeni test**, adını yapıcı koyar: render edilen landing'de
emoji sunumlu karakter yok — U+1F000–U+1FAFF, bölgesel göstergeler U+1F1E6–U+1F1FF, VS16 U+FE0F; ✓ ✗
gibi Dingbats işaretleri **serbest kalır**, karşılaştırma ve docket onları kullanır; KONTROL: eski 🏷️ ve
🇲🇹 geri konunca kırmızı — bu test M14-11'de bayrak için de geçerli olur); `TestLandingSteps_EveryStepIsRenderedInOrder`,
`TestLandingSteps_EveryStepRestsOnAProductFact` yeşil; her ikon `aria-hidden="true"`.

## M14-06 — Karşılaştırma: iki sütun her genişlikte

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(landing): keep both comparison columns on a phone`
- **Spec:** [README §6.5](../design/landing-redesign/README.md)

Bugün 640 px altında 2. sütun gizleniyor. Masaüstü/tablet 3 sütun, ✗/✓ kendi span'inde (sarılan satır
metnin altına hizalanır); telefonda her satır yığılır: aspect / ✗ device / ✓ Taptime, üstte iki çipli
gösterge. DOM metin sırası aspect → device → Taptime kalır.

**Kabul kriterleri.** `TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact`,
`TestLanding_ComparesMechanismsAndDeclaresNoWinner` yeşil; derlenmiş CSS'te karşılaştırma hücresine
`display: none` veren kural yok (**yeni test** ya da mevcut karşılaştırma testine iddia; KONTROL: eski
640 kuralıyla kırmızı); işaretler `aria-hidden`, kelimeler durumu taşır.

## M14-07 — Security: docket kartları ve gizlilik notu

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): draw the four proofs as dockets`
- **Spec:** [README §6.6](../design/landing-redesign/README.md)

Dört kanıt kartı panel docket'i (D2 keskin), yeşil kanıt eyebrow'u, ızgara 4 / 2 / 1 (960 / 540);
`LandingPrivacyNote` ızgara boyu, kalkan ikonuyla.

**Kabul kriterleri.** `TestLandingEvidence_EveryCardRestsOnAProductSource`,
`TestLandingEvidence_EveryPieceIsRendered`, `TestLandingEvidence_EveryProofLabelIsRendered`,
`TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact` yeşil; metin değişmez.

## M14-08 — YENİ "The panel" vitrini (NEW CLAIM)

- **Bağımlılık:** M14-01, M14-07 · **Araç:** yapıcı (`tappa-brand`) + işi yapmayan ayrı iddia denetçisi
- **Kırmızı çizgi:** §4.6 (vitrin "her clock-in kaydedilir" der; kanıtı karar motorudur)
- **Commit:** `feat(landing): show the panel the records land on`
- **Spec:** [README §6.7](../design/landing-redesign/README.md) · **Panolar:** `Landing`, `T-Landing`,
  `M-Landing` (`#panel` bölümü)

**Kapsam.**
- `#panel` bölümü Security'den sonra, Fits'ten önce; başlık, alt metin, üç minyatür (The Pass, The Spike,
  The Roll) ve dipnot. Minyatürler HTML/CSS, `<img>` yok; her biri `role="img"` + ad ("Sample screen: The
  Pass" …); 7 işaretli ink ray (Main.dc.html sırası), etkin hap 0 / 1 / 4. Kurallar `.lp` altında, panel
  sınıfları yeniden kullanılmaz. Gerekirse yeni `.templ` dosyası → `nonPanelTemplates`.
- **NEW CLAIM değerleri** `landingview.go`'da (README §6.7 tablosu, kelimesi kelimesine): P1 başlık
  "Every clock-in lands on The Pass.", P2 alt metin, P3–P5 üç başlık + açıklama. P0 eyebrow ve P6 dipnot
  mobilyadır (iddia değil). Örnek veri (adlar, saatler, uid'ler, trust, saatler) şablonda ya da tek bir
  "sample" işaretli değerde kalır; asla `Line`/`Fact` değeri olmaz.
- **Her iddia için (P1–P5):** `landingview.go` değeri + dayandığı Fact/Anchor + **türetme** (koddan,
  sabitten değil) + **mutasyon kontrolü** (kod olgusu kaldırılınca test kırmızı) + render kontrolü:
  - P1 → **yeni Fact** (önerilen ad `FactEveryDecidedTapIsRecorded`): `tap.Decide`'ın etki → verdict
    eşlemesinde kayıt yazmayan **tek** dal `redirect` (`internal/domain/tap/decide.go:44–50`), checkin yolu
    verdict'i `transactions`'a yazar, The Pass `ListPanelTransactions`'ı okur ve rota bağlı. Mutasyon: bir
    etkiyi (ör. `ignore`) kayıtsız yola çeviren bir eşleme ya da Pass rotasının kaldırılması → kırmızı.
  - P2 → üç panel rotasının bağlı olması + `FactFlaggedQueueDecidedByAManager` + `FactReportPerPersonAndVenue`.
  - P3 → işlem listesinin `DocketView` + verdict damgasıyla render edilmesi ve `ORDER BY t.occurred_at
    DESC, t.id DESC` (newest first). Mutasyon: sıralamanın `ASC`'ye çevrilmesi kırmızı.
  - P4 → `base:no-evidence-review` = `EffectReview` + `POST reviewHref` (`FactFlaggedQueueDecidedByAManager`).
  - P5 → `FactHoursExportAsCSV` (`route:GET reportsCSVHref`) + haftalık rapor rotası.

**Dosyalar.** `web/templates/pages/landingview.go`, `web/templates/pages/landing.templ` (ya da yeni
`landing_panel.templ`), `web/static/css/input.css`, `internal/handler/marketing_facts_test.go`,
`internal/handler/marketing_claims_test.go`, gerekirse `internal/handler/panelsource_test.go`.

**Kabul kriterleri.**
- `TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` yeni Fact'i hem türetilmiş hem iddia edilmiş
  bulur; `TestFactMechanisms_SayNoWhenTheFactIsAbsent` yeni Fact'in mekanizması için de "yok" der.
- **Yeni test** (adını yapıcı koyar): P1–P5'in her biri render edilen sayfada ve dayandığı olguyla; her
  biri için mutasyon kontrolü kırmızıyı görür. Denetçi en az P1 ve P3'ün mutasyonunu kendisi uygular.
- `TestMarketing_EveryInternalLinkResolves` (`#panel`), `TestLanding_MakesNoUnmeasuredNumericClaim`
  (örnek veride 99/100/% yok — TRUST 100 yok), `TestLanding_ShowsNoUserFacingInternalName`,
  `TestLanding_UsesOnlyPaletteTokens`, `TestTemplates_CarryNoInlineStyle` yeşil.
- Sayfada tek `<h1>`; minyatürlerde `<button>`, `<form>`, `<img>` yok.
- Örnek veri README §6.7 ile birebir (tek tenant, hero ile tutarlı; KM kişisi yok).

## M14-09 — Fits + pricing görünümü

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): align the audiences and pricing with the redesign`
- **Spec:** [README §6.8, §6.9](../design/landing-redesign/README.md)

Fits: yeşil kicker, yuvarlak green-on-green-lite çip, 2 / 1 sütun. Pricing: kurucu teklif satırı
saffron-lite zemin + saffron kenar, ink metin; includes listesinde ✓ sözde öğesi → check ikonu.

**Kabul kriterleri.** `TestLanding_PriceMatchesTheSchemaItIsCharged`,
`TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount`, `TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem`,
`TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact`, `TestLandingAudiences_EveryClaimRestsOnAProductAnchor`,
`TestLandingAudiences_EveryClaimIsActuallyRendered`, `TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist`
yeşil; metin ve sayılar değişmez.

## M14-10 — FAQ iki sütun (R-2'ye bağlı)

- **Bağımlılık:** M14-01 + **`claude/fix-landing-faq-api` (`1054fea`) bu dala ya da `main`'e birleşmiş
  olmalı.** Birleşmemişse kart ya bekler ya da o commit'i (cherry-pick, metni ve testleriyle birlikte)
  **aynen** taşır — Q4/Q5 metni, `FactNoIntegrationAPI`'nin emekliye ayrılması, üç API Fact'i ve "no API"
  tarayıcı testi. Kart o metni yeniden yazmaz, kısaltmaz.
- **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): lay the FAQ out in two columns`
- **Spec:** [README §6.10](../design/landing-redesign/README.md)

Masaüstünde iki sütun (solda yapışkan başlık, sağda liste), altında tek sütun; "+" açılınca "×" (yalnız
CSS, `[open]`). Ürün bütün maddeleri kapalı render eder (pano 1. maddeyi açık çizer — durum).

**Kabul kriterleri.** `TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer`,
`TestLandingFAQ_EveryAnswerRestsOnAProductFact` yeşil; Q4/Q5 R-2'nin metniyle kelimesi kelimesine aynı;
R-2'nin testleri bu dalda yeşil; yeni JS yok.

## M14-11 — CTA + altbilgi

- **Bağımlılık:** M14-02 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(landing): drop the flag emoji from the footer`
- **Spec:** [README §6.11, §6.12](../design/landing-redesign/README.md)

Altbilgi: "© 2026 Taptime · Malta" (bayrak emojisi yok), önünde küçük halka işareti (22); "tap in · tap out
· that's it"; Legal kartının bağlantı stili, 44 px hedefler; `<nav … aria-label="Legal and developers">`
yerinde. CTA metni ve tek düğmesi aynen; "Book a pilot" yok.

**Kabul kriterleri.** M14-05'in emoji testi bayrak için de yeşil; `TestMarketing_MountsEveryLegalDocumentAndLinksToIt`,
`TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`, `TestLanding_OffersTheWizardItMounts` yeşil.

## M14-12 — Demo diyaloğu

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): restyle the demo dialog`
- **Spec:** [README §6.13](../design/landing-redesign/README.md) · **Panolar:** `LandingDemo`,
  `M-LandingDemo` (`state` = soon | video)

İki durum panolardaki gibi: `soon` metni + CTA (telefonda tam genişlik), `video` yerel `<video controls
preload="metadata" playsinline>` + poster; kapatma düğmesi 44×44, autofocus halkası saffron; arka plan
ink/.62 (isteğe bağlı). İşaretleme ve kimlikler aynen.

**Kabul kriterleri.** Dokuz `marketingdemo_test.go` testi yeşil (README §4 listesi;
`TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse`,
`TestLandingDemo_WithNoRecordingTheDialogSaysSoAndRendersNoPlayer`,
`TestLandingDemo_WithARecordingTheDialogRendersThePlayerAndItsSource`,
`TestLandingDemo_PolicyNamesMediaOnlyWhenTheBuildCarriesTheDemo` dahil). `TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse` derlenmiş CSS'te açılış
animasyonunun `prefers-reduced-motion: no-preference` içinde olduğunu ve kapatma düğmesinin 44 px tabanını
okur — kural yeniden yazılırsa bu iki özellik korunur, dizge değişirse iddia aynı commit'te, niyeti
korunarak güncellenir.

---

## L3 — Kapanış

## M14-13 — Faz sonu: denetim, golden, ekran turu

- **Bağımlılık:** hepsi · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Kırmızı çizgi:** §9 (çalışan ekranları), §4.6 (vitrinin iddiası), §7 (log/gizlilik — landing sır taşımaz)
- **Commit:** `docs(brand): record the landing's tokens and the marketing lockup` (+ yalnız düzeltme commit'leri)

Tam `make check` (fmt + lint + `-race` test) ve `make audit` bir kez; bütün M14 diff'i üzerinde güvenlik
denetimi: CSP ve betik kümesi aynı (yalnız `landing.js`), üçüncü taraf istek yok, satır içi stil yok, yeni
çerez/depolama/konum yok, `tap.templ`/`result.templ`/`activate.templ`/`layout.*`/`components.*` diff'i
boş; golden kontrolü (`TestUnbrandedScreens_AreByteIdenticalToTheGolden` ve
`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` bayt-aynı; marka CSS golden'ı yalnız `.p-brand`
satırı kadar değişti); boşa düşmüş test taraması (`TestEveryNamedTestExists`); 390 / 834 / 1440 ekran turu:
landing (dört kayıt), demo diyaloğu iki durumuyla, signup, legal, API rehberi. Skill `tappa-brand`'e
landing token'ları ve pazarlama kilidinin özeti (tarihli not); README §5/§8 gerçekleşen sınıf adlarıyla
güncellenir.

**Kabul kriterleri.** Bulgu → düzeltme → yeni denetçi. Bittiğinde `state.md`; `main`'e birleştirme
**kullanıcının deploy kararıdır**.
