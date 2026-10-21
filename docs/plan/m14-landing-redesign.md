# M14 — Landing yeniden tasarımı: düzelt + uyum (kapsam B)

> **Durum:** plan (2026-10-21). Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya
> "yapıldı" yazılmaz.
>
> **Teslim paketi (okumadan başlama):** [docs/design/landing-redesign/README.md](../design/landing-redesign/README.md)
> — **Bölüm II (§10–§19): LP-6** (beş yeni genel sayfa + üç yeni landing bölümü; iddia envanterleri
> [lp6-inventory-a.md](../design/landing-redesign/lp6-inventory-a.md), [lp6-inventory-b.md](../design/landing-redesign/lp6-inventory-b.md))
> — **Bölüm III (§20–§28): LP-7 cilası** (sec-S35 onayı + Privacy bağı, landing sekiz bölüm ve taşınan iddialar,
> bantlar, next-step bağlantıları, yapışkan telefon çubuğu, hero kırpması, tek odak halkası + `scroll-padding`,
> Menu current, paylaşım kartları + OG, durum panoları; [lp7-share.md](../design/landing-redesign/lp7-share.md)); kartlar M14-25…M14-29
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
- **R-2 — FAQ API metni ayrı sevk edildi ve `main`'e BİRLEŞTİ (2026-10-21):** PR atknatk/tappa#6, merge
  `0e7c158`. M14-10 artık birleşmemiş bir dala değil, yalnız **`main`'in iş dalına birleştirilmesine**
  bağlıdır (bu not yazılırken `claude/admiring-brown-jw8hf1` `0e7c158`'i içermiyordu). Tarihçe: dal
  **`claude/fix-landing-faq-api`** `1054fea` ile **başladı** (`ab85193`'ün üstünde), şimdiden ardılı var (`88e5379`) ve
  yenileri gelebilir — bağımlılık **birleşme anındaki dal ucudur**, tek bir commit değil; `ab85193..<uç>`
  aralığı bütün olarak taşınır. Q4/Q5'i yeniden yazar, `FactNoIntegrationAPI` tuzağını emekliye ayırır,
  üç API Fact'i ve "`/v1` bağlıyken 'no API' diyen sayfa" tarayıcı testini ekler. M14 onu yeniden yazmaz;
  **M14-10 ona bağlıdır** ve `main` iş dalına **M14-08'den önce** alınmalıdır (LP-6'nın Fact ekleyen
  kartları M14-15…M14-22 için de aynısı): ikisi de `landingview.go` (Fact'ler)
  ve `internal/handler/marketing_facts_test.go`'ya dokunur — önce birleşirse çatışma iki taraflı değil,
  yalnız M14-08'in yeniden temellenmesi olur.

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
- **`make gen` + `make css`** her `.templ` / `input.css` değişikliğinden sonra. Commit edilen üretilmiş
  dosyalar yalnız `*_templ.go`'dur; `web/static/css/app.css` gitignore'dadır, `make css` (CI ve imaj
  derlemesi) üretir — commit edilmez.
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
| M14-04 | L1 | Hero sahnesi: telefon + raydan sarkan docket + damga üst satırda + landing.js'e üç hedef (R-1) + animasyonlar `no-preference` içine + creed | yapıcı (`tappa-brand`) + güvenlik (§9) | M14-01 |
| M14-05 | L1 | How it works: emoji → çizgi ikon (`Step.Icon` → ikon anahtarı) | yapıcı (`tappa-brand`) | M14-01 |
| M14-06 | L1 | Karşılaştırma: telefonda iki sütun da (yığılmış satırlar + gösterge) | yapıcı (`tappa-brand`) | M14-01 |
| M14-07 | L1 | Security: docket kartları + ızgara boyu gizlilik notu + kalkan ikonu | yapıcı (`tappa-brand`) | M14-01 |
| M14-08 | L2 | YENİ "The panel" vitrini: üç minyatür + NEW CLAIM değerleri, Fact'leri, testleri | yapıcı (`tappa-brand`) + ayrı iddia denetimi + güvenlik (§4.6) | M14-01, M14-07; önerilen: FAQ-API dalı önce birleşmiş |
| M14-09 | L1 | Pilot şeridi (telefonda yığılma) + fits + pricing görünümü: yuvarlak çip, kurucu teklif satırı, ✓ → ikon | yapıcı (`tappa-brand`) | M14-01 |
| M14-10 | L1 | FAQ iki sütun (+ R-2 bağımlılığı) | yapıcı (`tappa-brand`) | M14-01, **`main` (`0e7c158` dahil) iş dalına birleşmiş** |
| M14-11 | L1 | CTA + altbilgi: bayrak emojisi yok, halka işareti, Legal kartı bağlantı stili | yapıcı (`tappa-brand`) | M14-02 |
| M14-12 | L1 | Demo diyaloğu yeniden stili (`soon` / `video`); odak halkası M14-25'inki (önce koşarsa aynı değerleri kendisi verir) | yapıcı (`tappa-brand`) | M14-01 |
| M14-13 | L3 | Faz sonu: tam `make check` + `make audit`, güvenlik denetimi, golden kontrolü, 390/834/1440 ekran turu (M14-24 ile **tek** kapanış olarak koşar) | denetçi + güvenlik | hepsi |
| M14-14 | P0 | **LP-6** paylaşılan genel kabuk: başlık sayfa bağlantıları, telefonda `<details>` Menu, altbilgi Product/Legal sütunları — `marketingChromeWith` + landing; `PublicPages` listesi; `TestLandingNav_…` bilinçli yeniden yazımı | yapıcı (`tappa-brand`) | M14-02, M14-03 |
| M14-15 | P2 | **LP-7: `/product`'a taşındı** — bölüm 01 sahnesi: iki telefon (tap + flag sonucu) + "For your team" noktaları (land-T2…T10) | yapıcı (`tappa-brand`) + güvenlik (§9) | M14-04, M14-18 |
| M14-16 | P2 | **LP-7: `/product`'a taşındı** — "A shift, start to finish" (altı docket, ray; 07 Rules'tan sonra) | yapıcı (`tappa-brand`) | M14-08, M14-18 |
| M14-17 | P2 | **LP-7: `/product`'a taşındı** — Integrations (CSV · API · webhook; CTA'dan önceki son bölüm, yalnız orada). Landing bağlantıları M14-26'da | yapıcı (`tappa-brand`) | M14-10, M14-18 |
| M14-18 | P2 | `/product` (ürün turu, yedi bölüm, üç yeni minyatür; 15/16/17'nin bölümlerine yer) | yapıcı (`tappa-brand`) + ayrı iddia denetimi + güvenlik (§9) | M14-08, M14-14 |
| M14-19 | P2 | `/pricing` (sunucuda hesaplanan ekip tablosu, fatura SSS) | yapıcı (`tappa-brand`) + ayrı iddia denetimi | M14-09, M14-10 (`main` birleşmesi: `FactAPIEnabledPerBusinessByTaptime`), M14-14 |
| M14-20 | P2 | `/security` (sec-S35 **LP-7'de onaylandı**: yalnız sunucu cümlesi, Privacy'nin bekleyen barındırma olgusuyla **aynı commit'te**) | yapıcı (`tappa-brand`) + ayrı iddia denetimi + **`tappa-security-auditor`** | M14-07, M14-14 |
| M14-21 | P2 | `/for/restaurants` | yapıcı (`tappa-brand`) + ayrı iddia denetimi | M14-14, M14-20 (paylaşılan Fact'ler) |
| M14-22 | P2 | `/for/production` | yapıcı (`tappa-brand`) + ayrı iddia denetimi + güvenlik (§9) | M14-14, M14-20 |
| M14-23 | P1 | Signup/Legal yeniden başlık: LP-6 kabuğu, gövde bayt-aynı + iki gövde kusuru | yapıcı (`tappa-brand`) | M14-14 |
| M14-25 | P0 | **LP-7** tek odak halkası (2 px ink/paper, 2 px offset; `input.css:1799`'un safran halkası yerine) + `scroll-padding` (SC 2.4.11) — bütün genel sayfalar, Signup ve Legal dahil | yapıcı (`tappa-brand`) | M14-14 |
| M14-26 | P1 | **LP-7** landing sekiz bölüm + bant ritmi (paper/porcelain/ink) + bölüm başına tek "next step" bağlantısı (`.more`), `.fitcard` bütün-kart bağlantıları | yapıcı (`tappa-brand`) | M14-14, M14-25 |
| M14-27 | P1 | **LP-7** telefonda yapışkan "Start free" çubuğu (`.mbar`, yalnız CSS, Signup'ta yok) | yapıcı (`tappa-brand`) | M14-14, M14-25 |
| M14-28 | P2 | **LP-7** paylaşım kartları (`web/static/og/*.png`) + başlık/açıklama/OG/twitter etiketleri; api-N1 | yapıcı (`tappa-brand`) + ayrı iddia denetimi + güvenlik (§7) | M14-14, M14-18…M14-22 (sayfalar bağlı) |
| M14-29 | P2 | **LP-7** durum panoları = etkileşim spec'i: hover/pressed/disabled/visited/current/open her genel denetimde | yapıcı (`tappa-brand`) | M14-25…M14-27 |
| M14-24 | P3 | Faz sonu uzantısı (M14-13 ile tek kapanış): beş sayfa, 29 pano ekran turu, kabuk testi, LP-7 maddeleri, güvenlik | denetçi + güvenlik | hepsi (M14-25…29 dahil) |

L1 kartları birbirinden bağımsızdır; her biri kendi bağımlılıkları bitince tek başına sevk edilebilir
(dal üzerinde, CI yeşil). Her kart üç genişliği **birlikte**, tek DOM ile taşır.

---

## L0 — Temel ve başlık

## M14-01 — `.lp` temeli: token'lar ve mono ağırlıkları

- **Bağımlılık:** — · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): keep the landing block on palette tokens and two mono weights`
- **Spec:** [README §5](../design/landing-redesign/README.md)

**Amaç.** Görünür düzeni değiştirmeden `.lp` bloğunu palete indirmek: README §5 tablosundaki palet dışı
**18 hex** (`--ink-soft`, `--green-deep`, `#F4F6F1`, `#9AA79C`, `#D98B7B`, `#9DB3A5`, `#B9C8BC`, `#33473B`,
`#24352B`, `#E2DFC9`, `#C9C4A8`, `#4B5C50`, `#CFE6D8`, `#6FA88B`, `#7A9484`, `#F2F5EF`, `#fff`, `#E9EFE9`) ve
**5 palet dışı `rgba()`** (`rgba(255,255,255,.7)`, `rgba(255,255,255,.14)`, `rgba(15,25,18,.35)`,
`rgba(15,25,18,.62)`, `rgba(20,30,22,.18)`) token + opaklık olur; IBM Plex Mono'nun 500/600 kullanımları (heroCreed, eyebrow, step `.n`, link `.proof`, fit
`.k`, `d-*`, ledger `th`, strip `b`) 400 ya da 700 olur.

**Dosyalar.** `web/static/css/input.css` (`.lp` bloğu), `internal/brand/theme_test.go` (`:955` occurrence
golden'ı). (`app.css` gitignore'dadır; `make css` ile yerelde üretilir, commit edilmez.)

**Kabul kriterleri.**
- `.lp` bloğunda palet token'ları (`--ink`, `--green`, `--green-lite`, `--porcelain`, `--paper`,
  `--saffron`, `--saffron-lite`, `--tomato`, `--line`, `--mint`) dışında renk yok: ne hex ne de bir
  token'ın kanallarından başka bir `rgb()`/`rgba()` (izinli: `rgb(21 34 25 / .8)` gibi token kanalları +
  opaklık). Grep ile gösterilir (hex, `rgb(`, `rgba(`); token tanımlarının kendisi hariç.
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

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Kırmızı
  çizgi:** §9 (tap ekranı **çizilir, değiştirilmez**)
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
- **LP-7 hero dengesi (README §26, §6.2'nin ölçülerinin yerine):** telefon plaketten **dar** (214 / 204 / 150 px;
  plaket 226 / 214 / 176) ve **cihazın üstüne kırpılmış** (alt çerçeve yok, ekran alt kenardan taşar, gölge
  yukarı; kesik 4 px ink ekran-kenarı çizgisi + kesikli kırpma işareti ink/35 ile biter). Sahne bir ızgara:
  plaket · NFC yayları · telefon tek satırda (dikey ortalı), docket rayından plaketin altında — göz plaket →
  dokunuş → docket okur. Kırpma sabit yükseklik + `overflow: hidden`, kırılım başına sabit `.lp` sınıfları.
  Kayıtların KM mekânı "KM Plant" (LP-6).
- Telefon: `tap.templ`'in landing'e özel kopyası — durum çubuğu, "taptime" + "punchless", docket
  ("TAPPING" / "Hello {name}" / mekân), tek "Tap" (64 px, rounded-sm, yeşil, text-xl 400) — `<span>`,
  `<button>` değil. 375 genişlikte gerçek CSS ölçüleri + sabit ölçek sınıfı (.61 / .58 / .384). Kap
  `role="img"` + genel ad ("The employee's own phone showing the tap screen"). Selamlama `<p>`/`<span>`;
  `pages.TapHeading` kullanılmaz.
- Plaket: venue · uid satırı `id="pPlaque"`; telefonda `id="pName"`, `id="pLoc"`. Üçü koşulsuz.
- `landing.js`: her kayda `plaque` alanı (README §6.2 tablosu: KF ST JULIANS · 91AC-7E55, **KM PLANT ·
  2F04-1B9A** (LP-6: mekân "KM Facility" → "KM Plant"), RUSTY BAR · 6D31-C8E2); aynı 3,8 sn aralığında üç ek `textContent` yazımı. Başka değişiklik
  yok; demo yarısına dokunulmaz. Dosyanın baş yorumu ("IT TOUCHES NOTHING BUT FOUR TEXT NODES", "carried
  over … UNCHANGED") yeniden yazılır: artık **yedi** metin düğümü; fazladan üçü referanstan değil,
  kullanıcının R-1 kararından (2026-10-21).
- `landingScene` yorumu (`landing.templ:184–195`, "a drawing, not a record") yeni iki uid'i ve telefonu
  kapsar.
- **Hareket azaltma (bugünkü durum yanlış yönde).** Bugün `tapring` ve `printout` `.lp` bloğunun **en üst
  düzeyinde** (`input.css:~1862`, `~1867`); `.lp .lp-docket` **kırpılmış başlar** (`clip-path: inset(0 0
  100% 0)`, `~1866`) ve onu yalnız `prefers-reduced-motion: reduce` bloğu (`~1987–1990`) geri alır — o
  blok düşerse hareket azaltmak isteyen okur bileti hiç görmez. Port: iki animasyon **ve** kırpılmış
  başlangıç durumu `@media (prefers-reduced-motion: no-preference)` içine taşınır; dışında docket
  varsayılan olarak tam görünür, reduce bloğu gereksizleşir. Print-out docket'i raydan **aşağı** açar.
- **Creed:** her ifade `nowrap`, "Just tap." 700 ink, üstü çizili sözcükler ink/.70 + tomato çizgi
  (400 ağırlığı M14-01'de).

**Dosyalar.** `web/templates/pages/landing.templ`, `web/static/js/landing.js`, `web/static/css/input.css`,
`internal/handler/marketing_test.go` (`TestLanding_ReducedMotionLeavesTheDocketVisible` genişler; yeni
test) ya da `marketingdemo_test.go`, üretilen `landing_templ.go`.

**Kabul kriterleri.**
- **Yeni test** (adını yapıcı koyar): `landing.js`'in aradığı her kimlik (`getElementById('…')` **ya da**
  `querySelector('#…')` — bugün `dName`, `dType`, `dTime`, `dLoc`, `demoOpen`, `demoDialog`, `#demoClose`;
  port sonrası + `pName`, `pLoc`, `pPlaque`) render edilen sayfada **tam bir kez** var; kimlik listesi
  betikten okunur, elle yazılmaz; KONTROL: şablondan `pPlaque` silinince kırmızı. Bugün dönüşün
  hedeflerini sabitleyen test yok.
- `TestLanding_ReducedMotionLeavesTheDocketVisible` bugün yalnız signup'ın `docket-print` kuralını tarar;
  **genişletilir** (ya da tarif edilen yeni bir test): `printout`, `tapring` ve `.lp-docket`'in `clip-path`
  başlangıcı derlenmiş ve kaynak CSS'te yalnız `prefers-reduced-motion: no-preference` bloğu içinde;
  KONTROL: bugünkü en üst düzey kural geri konunca kırmızı. Signup iddiası aynen kalır.
- `tappa-security-auditor` geçişi (§9 — tap ekranı çizilir, değiştirilmez; `docs/plan/README.md`):
  `tap.templ`/`result.templ`/`activate.templ` diff'i boş, telefon kopyası canlı bileşeni çağırmaz,
  landing.js yeni yetenek (istek, çerez, depolama, konum) kazanmadı.
- Sayfada tek `<h1>`; telefonun içinde başlık öğesi yok; `role="img"` + `aria-label` telefonda.
- `TestLanding_CarriesTheSlogan`, `TestLandingDemo_TheScriptDegradesInsteadOfBreaking`, `TestLanding_MakesNoUnmeasuredNumericClaim`,
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

**Dosyalar.** `web/templates/pages/landingview.go` (`Step.Icon` tipi), `web/templates/pages/landing.templ`,
`web/static/css/input.css`, `internal/handler/marketing_claims_test.go`, yeni emoji testi için
`internal/handler/marketing_test.go`, üretilen `landing_templ.go`.

**Kabul kriterleri.**
- Sayfada emoji yok (**yeni test**, adını yapıcı koyar: render edilen landing'de
emoji sunumlu karakter yok — U+1F000–U+1FAFF, bölgesel göstergeler U+1F1E6–U+1F1FF, VS16 U+FE0F; ✓ ✗
gibi Dingbats işaretleri **serbest kalır**, karşılaştırma ve docket onları kullanır; KONTROL: eski 🏷️ ve
🇲🇹 geri konunca kırmızı — bu test M14-11'de bayrak için de geçerli olur).
- `TestLandingSteps_EveryStepIsRenderedInOrder` (`marketing_claims_test.go:590`) bugün `s.Icon`'un render
  edilen metinde geçmesini ister — emoji kalkınca kırmızı olur. İddia **aynı commit'te** değişir, niyeti
  korunarak: her adım **kendi** ikonunu render eder (ör. adımın SVG'sinde anahtara özgü bir `data-icon="…"`
  işareti, adım sırasıyla ve adımın kendi bölümünde aranır); KONTROL: iki anahtar yer değiştirince ya da
  bir SVG düşünce kırmızı.
- `TestLandingSteps_EveryStepRestsOnAProductFact` yeşil; her ikon `aria-hidden="true"`.

## M14-06 — Karşılaştırma: iki sütun her genişlikte

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(landing): keep both comparison columns on a phone`
- **Spec:** [README §6.5](../design/landing-redesign/README.md)

Bugün 640 px altında 2. sütun gizleniyor. Masaüstü/tablet 3 sütun, ✗/✓ kendi span'inde (sarılan satır
metnin altına hizalanır); telefonda her satır yığılır: aspect / ✗ device / ✓ Taptime, üstte iki çipli
gösterge. DOM metin sırası aspect → device → Taptime kalır.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingLedger`), `web/static/css/input.css`,
`internal/handler/marketing_facts_test.go` (karşılaştırma iddiası ya da yeni test), üretilen
`landing_templ.go`.

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

**Dosyalar.** `web/templates/pages/landing.templ` (`landingSecurity`), `web/static/css/input.css`, üretilen
`landing_templ.go`.

**Kabul kriterleri.** `TestLandingEvidence_EveryCardRestsOnAProductSource`,
`TestLandingEvidence_EveryPieceIsRendered`, `TestLandingEvidence_EveryProofLabelIsRendered`,
`TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact` yeşil; metin değişmez.

## M14-08 — YENİ "The panel" vitrini (NEW CLAIM)

- **Bağımlılık:** M14-01, M14-07; **önerilen:** FAQ-API dalı (R-2) önce birleşmiş — ikisi de
  `landingview.go` ve `marketing_facts_test.go`'ya dokunur, önce birleşmezse çatışma beklenir
- **Araç:** yapıcı (`tappa-brand`) + işi yapmayan ayrı iddia denetçisi + agent `tappa-security-auditor`
  (§4.6: "her clock-in kaydedilir" iddiası karar motorunun etki → verdict eşlemesine dayanır;
  `docs/plan/README.md`)
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
- **Commit:** `style(landing): align the pilot strip, audiences and pricing with the redesign`
- **Spec:** [README §6.8, §6.9](../design/landing-redesign/README.md)

Pilot şeridi: paper/.55 zemin (M14-01), telefonda ortalı ve alt alta. Fits: yeşil kicker, yuvarlak
green-on-green-lite çip, 2 / 1 sütun. Pricing: kurucu teklif satırı
saffron-lite zemin + saffron kenar, ink metin; includes listesinde ✓ sözde öğesi → check ikonu.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingStrip`, `landingFits`, `landingPricing`),
`web/static/css/input.css`, üretilen `landing_templ.go`.

**Kabul kriterleri.** `TestLanding_PriceMatchesTheSchemaItIsCharged`,
`TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount`, `TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem`,
`TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact`, `TestLandingAudiences_EveryClaimRestsOnAProductAnchor`,
`TestLandingAudiences_EveryClaimIsActuallyRendered`, `TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist`
yeşil; metin ve sayılar değişmez.

## M14-10 — FAQ iki sütun (R-2'ye bağlı)

- **Bağımlılık:** M14-01 + **`main`'in iş dalına birleştirilmesi.** FAQ-API düzeltmesi
  (`claude/fix-landing-faq-api`, `1054fea` … `88e5379` …) `main`'e **birleşti** — PR atknatk/tappa#6,
  merge `0e7c158` (2026-10-21). Kart `main`'i iş dalına alır (`git merge origin/main`; çatışma olursa
  düzeltmenin metni ve testleri **aynen** kalır — Q4/Q5, `FactNoIntegrationAPI`'nin emekliye ayrılması,
  üç API Fact'i, "no API" tarayıcı testi ve kontrolleri). Kart o metni yeniden yazmaz, kısaltmaz. M14-08
  aynı dosyalara dokunur — `main` M14-08'den önce alınmalı.
- **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): lay the FAQ out in two columns`
- **Spec:** [README §6.10](../design/landing-redesign/README.md)

Masaüstünde iki sütun (solda yapışkan başlık, sağda liste), altında tek sütun; "+" açılınca "×" (yalnız
CSS, `[open]`). Ürün bütün maddeleri kapalı render eder (pano 1. maddeyi açık çizer — durum).

**Kabul kriterleri.** `TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer`,
`TestLandingFAQ_EveryAnswerRestsOnAProductFact` yeşil; Q4/Q5 R-2'nin metniyle kelimesi kelimesine aynı;
yeni JS yok.
- `git merge-base --is-ancestor 0e7c158 HEAD` 0 döner (FAQ-API düzeltmesinin `main`'deki merge commit'i
  iş dalında).
- R-2'nin tarayıcı testi — Landing önekli `…ClaimsNoAbsentAPIWhileV1IsMounted`; önekiyle yazılmadı, çünkü
  `TestEveryNamedTestExists` bu dalda henüz olmayan bir test adını reddeder — ve kontrolleri (eski
  "no API" cevabı geri konunca kırmızı) bu dalda yeşil; dalın öteki testleri de.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingFAQ`), `web/static/css/input.css`, üretilen
`landing_templ.go` (+ R-2 taşınıyorsa onun dosyaları, değiştirilmeden).

## M14-11 — CTA + altbilgi

- **Bağımlılık:** M14-02 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(landing): drop the flag emoji from the footer`
- **Spec:** [README §6.11, §6.12](../design/landing-redesign/README.md)

Altbilgi: "© 2026 Taptime · Malta" (bayrak emojisi yok), önünde küçük halka işareti (22); "tap in · tap out
· that's it"; Legal kartının bağlantı stili, 44 px hedefler; `<nav … aria-label="Legal and developers">`
yerinde. CTA metni ve tek düğmesi aynen; "Book a pilot" yok.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingCTA`, `landingFooter`), `web/static/css/input.css`,
üretilen `landing_templ.go`.

**Kabul kriterleri.** M14-05'in emoji testi bayrak için de yeşil; `TestMarketing_MountsEveryLegalDocumentAndLinksToIt`,
`TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`, `TestLanding_OffersTheWizardItMounts` yeşil.

## M14-12 — Demo diyaloğu

- **Bağımlılık:** M14-01 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(landing): restyle the demo dialog`
- **Spec:** [README §6.13](../design/landing-redesign/README.md) · **Panolar:** `LandingDemo`,
  `M-LandingDemo` (`state` = soon | video)

İki durum panolardaki gibi: `soon` metni + CTA (telefonda tam genişlik), `video` yerel `<video controls
preload="metadata" playsinline>` + poster; kapatma düğmesi 44×44, autofocus halkası **README §25 / M14-25'in tek halkası** (2 px ink — ink/yeşil zeminde
paper —, 2 px offset; safran **yok**); arka plan ink/.62 (isteğe bağlı). İşaretleme ve kimlikler aynen.

> **Sıra notu (LP-7):** bu kart M14-25'ten önce koşabilir. Öyleyse kapatma düğmesine M14-25'in halkasını
> şimdiden verir (aynı değerler) ve `.lp :focus-visible` safran kuralına dayanmaz; M14-25 kuralı genele
> taşırken bu düğme değişmeden kalır. Denetçi, düğmenin odak halkasının ink/paper 2 px / 2 px olduğunu doğrular.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingDemoDialog`), `web/static/css/input.css`,
gerekirse `internal/handler/marketingdemo_test.go` (yalnız niyeti korunan dizge güncellemesi), üretilen
`landing_templ.go`.

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

- **Bağımlılık:** hepsi (M14-01…12, M14-14…23 **ve** M14-25…29) · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Not (LP-6):** M14-13 ile M14-24 ayrı iki tur değil, **tek** kapanıştır — tam `make check` ve
  `make audit` bütün M14 için bir kez koşar.
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

---

# Bölüm II — LP-6: yeni genel sayfalar ve landing bölümleri (kullanıcı, 2026-10-21)

> **Spec:** [README Bölüm II, §10–§19](../design/landing-redesign/README.md) · iddia satırları:
> [lp6-inventory-a.md](../design/landing-redesign/lp6-inventory-a.md) (landing ekleri, `/product`, `/pricing`,
> kabuk, yeniden başlık) ve [lp6-inventory-b.md](../design/landing-redesign/lp6-inventory-b.md) (`/security`,
> `/for/restaurants`, `/for/production`, port fizibilitesi). Panolar: `Product`, `Pricing`, `Security`,
> `ForRestaurants`, `ForProduction` × masaüstü/`T-`/`M-`, güncellenen `Landing` ×3, `LandingDemo` ×2,
> `Signup` ×3, `Legal` ×3 (telefon panolarında `menu` = closed | open).

**Karar (LP-6).** Kullanıcı *"dahada landingi cogalt . daha baska pageler olsun"* dedi ve önerilen sayfaların
**hepsini** seçti. Kurallar LP-1…LP-5 ile aynı (D1, mevcut iddialar kelimesi kelimesine, her yeni cümle
NEW CLAIM + Fact, palet, D2, Plex Mono 400/700, emoji yok, `img-src` yok, satır içi stil yok, landing.js
dışında yeni JS yok). Yeni okuma, yeni migration yok; yeni olan beş **genel rota**dır.

**Her LP-6 iddia kartının ortak kuralı (M14-08 deseni).** Kartın envanter satırlarındaki her NEW CLAIM:
1. `landingview.go`'da (ya da sayfanın `pages` içindeki görünüm dosyasında) bir değer — şablonda serbest
   metin değil;
2. dayandığı Fact/Anchor/Source (envanterde mevcut ya da önerilen ad; README §18'deki "=" çiftleri **tek**
   Fact'tir);
3. **koddan türetme** (sabit değil: rota kaydı, politika tablosu, config varsayılanı, sorgu metni, şablon);
4. **mutasyon kontrolü** — kod olgusu kaldırılınca/çevrilince test kırmızı; denetçi en az iki tanesini
   kendisi uygular;
5. render kontrolü (değer sayfada).
`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` ve `TestFactMechanisms_SayNoWhenTheFactIsAbsent`
her yeni Fact'i kapsar (genel sayfalar da "claimed" taramasına girer). Örnek veri, figür etiketleri ve
dürüstlük satırları mobilyadır. `FactAPI*` Fact'leri ancak `main` (`0e7c158`) iş dalına alındıktan sonra
vardır. Her yeni sayfa için ayrıca: rota testi, CSP testi, satır içi stil yok, ölçülmemiş sayı yok
(`TestLanding_MakesNoUnmeasuredNumericClaim`'in tarayıcısı yeni yollara da), iç ad yok, tek `<h1>`,
`nonPanelTemplates` sınıflandırması, 390/834/1440 ekran görüntüsü (telefonda Menu kapalı ve açık).

## P0 — Kabuk

## M14-14 — Paylaşılan genel kabuk (başlık, telefon Menu'su, altbilgi)

- **Bağımlılık:** M14-02, M14-03 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `feat(marketing): share one public header, menu and footer across the public pages`
- **Spec:** [README §10 LP-6.3, §11, §12](../design/landing-redesign/README.md), envanter A §0

**Amaç.** Her genel sayfada (landing, beş yeni sayfa, signup, legal, API rehberi) aynı kabuk: başlıkta
Product · Security · Pricing **sayfa** bağlantıları (geçerli sayfada `aria-current="page"`), Sign in, Start
free; telefonda yerel `<details>` "Menu"/"Close" ("Pages" + altı bağlantı, 48 px satırlar, yapışkan
başlığın altında üst üste biner); altbilgide marka bloğu + marketingChrome'un iddia paragrafı (aynen) +
Product sütunu (Product tour, Security, Pricing, For restaurants & bars, For production & facilities, API
docs) + Legal sütunu.

**Kapsam.**
- Beş rota sabiti handler'da (`signupPath`/`APIGuidePath` deseni) ve **tek** bir `PublicPages` listesi
  (yol, etiket, kısa etiket) — başlık, Menu ve altbilgi ondan beslenir. Bu kart rotaları **bağlamaz**
  (sayfa kartları bağlar). Bağlantılar kart sırasıyla görünür kılınır: henüz bağlanmamış bir sayfanın
  bağlantısı render edilmez, böylece `TestMarketing_EveryInternalLinkResolves` her ara commit'te yeşil
  kalır.
- `marketingChromeWith` (Tailwind) ve `landingNav`/`landingFooter` (`.lp`) aynı bağlantıları aynı sırada
  basar; `layout.*` değişmez. API rehberinin baytları **bilerek** değişir; `landing.templ:580–585`
  yorumu yeni ölçümle yeniden yazılır (README §7, §12).
- `LandingNav` paylaşılan genel nav olur; ilk `<nav>` tek kalır, `<details>` ondan sonra ya da içinde.
- **LP-7 Menu geçerli sayfa stili (README §26):** `.mlink[aria-current="page"]` tappa-green metin + 3 px
  green iç çubuk (`box-shadow: inset 3px 0 0`), paper'da 7,73:1. Başlığın geçerli bağlantısı LP-6 stilinde.
  Kabul: her-genel-sayfa testi Menu'de de `aria-current`'ı tam bir ve doğru bağlantıda bulur.
- Genel sayfaların stil kapsamı kararı (`.lp` paylaşımı ya da ortak kapsam) bu kartta verilir ve README
  §11'e yazılır.

**Dosyalar.** `web/templates/pages/landing.templ`, `web/templates/pages/landingview.go` (`PublicPages`,
`LandingNav`), `internal/handler/marketing.go` (rota sabitleri), `web/static/css/input.css`,
`internal/handler/marketing_facts_test.go`, `internal/handler/marketing_test.go`,
`internal/handler/apiguidepage_test.go`, `internal/handler/signup_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- `TestLandingNav_EveryLinkPointsAtASectionOnThePage` **bilinçli yeniden yazılır**, niyeti korunarak:
  ilk `<nav>`'daki her çubuk bağlantısı **bağlı bir genel sayfaya** gider (çapalar değil), geçerli sayfa
  işaretlidir, signup ve sign-in bağlantıları çubuktadır; KONTROL: bağlanmamış bir yola bağlantı kırmızı.
  Ad değişirse yeni adı yapıcı koyar, eski ad belgelerden aynı commit'te temizlenir
  (`TestEveryNamedTestExists`).
- **Yeni test** (adını yapıcı koyar) — her-genel-sayfa-bağlantıları: landing, signup adımları, dört legal
  sayfası, rehberin iki dili ve (bağlandıkça) beş yeni sayfa aynı başlık bağlantılarını (`aria-current`
  en çok birinde ve doğru olanda), altı bağlantılı Menu'yu ve altbilginin Product + Legal sütunlarını
  taşır; KONTROL: bir sayfadan bir bağlantı düşürülünce kırmızı.
- Menu JS'siz (`<details>`/`<summary>`); `TestLandingDemo_TheScriptDegradesInsteadOfBreaking` ve betik
  kümesi aynı; dokunma hedefleri ≥ 44 px.
- `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`, `TestAPIGuidePages_AreTheGuideInTheMarketingChrome`,
  `TestAPIGuidePages_FitAPhone`, `TestSignupSurface_EverySignInLinkGoesToTheSignInPage`,
  `TestSignup_MountsExactlyTheRoutesTheLandingPageOffers`, `TestMarketing_MountsEveryLegalDocumentAndLinksToIt`,
  `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce`, `TestTemplates_CarryNoInlineStyle`,
  `TestLanding_UsesOnlyPaletteTokens` yeşil; `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` ve
  `TestUnbrandedScreens_AreByteIdenticalToTheGolden` bayt-aynı.

## P1 — Landing'in yeni bölümleri

## M14-15 — `/product` bölüm 01: iki telefon + "For your team" (LP-7: landing'den taşındı)

> **LP-7:** bu blok landing'de **render edilmez** (README §21). Yeri `/product`'ın 01 "The tap" bölümü:
> plaket + iki telefon sahnesi, altında h3 "One screen, one button." + dört nokta + dipnot. İd'ler `land-`
> önekini korur. `#team` çapası `/`'da yoktur.

- **Bağımlılık:** M14-04 (telefon çizimi), M14-18 (sayfa) · **Araç:** yapıcı (`tappa-brand`) + agent
  `tappa-security-auditor` (§9: tap ve sonuç ekranı **çizilir**, değiştirilmez)
- **Commit:** `feat(marketing): show the employee's two screens on the product tour`
- **Spec:** README §13, §21, envanter A §1.1 (land-T1–land-T10) ve LP-7 taşıma tablosu

İki telefon gerçek CSS ölçülerinde: tap ekranı ve **flag sonucu** (`result.templ` flag dalı, yön `in`,
practice değil; damga, kapanış satırı ve "You can close this page." ürünün kendi metni; not =
`base:no-evidence-review` cümlesi). Örnek: Grace Attard · KF Msida · 09:41 · Trust 20. Her telefon `role="img"`
+ ad, içinde başlık yok; 390'da alt alta (~0,62 ölçek). Genel sayfaya özel kopyalar (canlı bileşenler
çağrılmaz).

**Dosyalar.** `web/templates/pages/product.templ`, `web/templates/pages/landingview.go`,
`web/static/css/input.css`, `internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.** land-T2–land-T9'un her biri ortak iddia kuralıyla (yeni Fact'ler: `FactTapScreenHasOneButton`,
`FactTapScreenNamesTheTappedVenue`, `FactCountedTapClosesWithoutAButton`, `FactFlaggedResultSaysItNeedsApproval`;
mevcutlar: `FactTapPageNeedsNoApp`, `FactTapPageGreetsByName`, `FactTapButtonSizedForAWetHand`). Çizilen sonuç
metinleri `result.templ`'in dizgeleriyle **aynı**: yeni test onları şablonun render'ından okur ve
karşılaştırır; KONTROL: şablondaki kapanış cümlesi değişince kırmızı. `tap.templ`/`result.templ` diff'i
boş; `TestUnbrandedScreens_AreByteIdenticalToTheGolden` bayt-aynı; sayfada emoji yok (M14-05 testi).
İddia testleri `/product`'ı okur; `/` bu metinleri **taşımaz** (yeni iddia: `/`'da land-T2 cümlesi yok).

## M14-16 — `/product`: "A shift, start to finish" (LP-7: landing'den taşındı)

> **LP-7:** kendi bölümü olarak `/product`'ta, 07 Rules'tan sonra, Integrations'tan önce. `#shift` `/`'da yok.

- **Bağımlılık:** M14-08 (`FactEveryDecidedTapIsRecorded`), M14-18 · **Araç:** yapıcı (`tappa-brand`)
- **Commit:** `feat(marketing): walk one shift from the invite to the payroll file`
- **Spec:** README §13, §21, envanter A §1.2 (land-S1–land-S9)

Ink ray üzerinde altı docket (masaüstü 1×6, tablet 2×3, telefon numaralı dikey liste); 04 FLAGGED; aktör
çipleri mobilya; eğimler `nth-child` sınıfları.

**Dosyalar.** `web/templates/pages/product.templ`, `web/templates/pages/landingview.go`,
`web/static/css/input.css`, `internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.** land-S2–land-S9 ortak iddia kuralıyla (yeni: `FactActivationTapRecordsNoHours` — aktivasyon
dokunuşu `transactions` satırı yazmaz; türetme `internal/handler/activate.go`'nun tamamlama yolundan;
mevcut: `FactPeopleAreInvitedFromTheDashboard`, `FactFlaggedQueueDecidedByAManager`, `FactHoursExportAsCSV`,
`FactEveryDecidedTapIsRecorded`). land-S7 "never approved in silence" `base:no-evidence-review` + motorun
varsayılanına dayanır; KONTROL: varsayılan etki `allow`'a çevrilince kırmızı.

## M14-17 — `/product`: Integrations (LP-7: yalnız orada)

> **LP-7:** Integrations yalnız `/product`'ta, CTA'dan önceki son bölüm; bölüm tek bir "Read the API docs"
> bağlantısıyla biter (`.more`, M14-26 stili). Landing'in yeni sayfalara bağlantıları **M14-26**'dadır.

- **Bağımlılık:** M14-10 (`main` + FAQ-API Fact'leri), M14-18 · **Araç:** yapıcı (`tappa-brand`)
- **Commit:** `feat(marketing): show where the hours go after Taptime`
- **Spec:** README §13, §21, §23, envanter A §1.3 (land-I1–land-I7)

Üç hücre (CSV · integration API · webhooks) + not çubuğu; mono çipler gerçek tanımlayıcılar.

**Dosyalar.** `web/templates/pages/product.templ`, `web/templates/pages/landingview.go`,
`web/static/css/input.css`, `internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.** land-I2–land-I6 ortak iddia kuralıyla (mevcut `FactAPIWritesPeopleAndLocations`,
`FactAPIFeedsEveryPunch`, `FactAPIEnabledPerBusinessByTaptime` — webhook'lara genişletilir —,
`FactHoursExportAsCSV`, `FactBillingExportAsCSV`, `FactReportPerPersonAndVenue`; yeni
`FactWebhookIsOneSignedEndpoint`). **land-I7 çipleri sabitlerden**: `/v1` yolları rota kayıtlarından,
`X-Taptime-Signature` `internal/webhook/signing.go`'nun başlık sabitinden, "Download CSV" ürün düğmesinin
dizgesinden; KONTROL: bir rota yeniden adlandırılınca kırmızı. FAQ-API düzeltmesinin "no API" tarayıcı
testi yeşil. `TestMarketing_EveryInternalLinkResolves` yeşil.

## M14-23 — Signup/Legal yeniden başlık

- **Bağımlılık:** M14-14 · **Araç:** yapıcı (`tappa-brand`)
- **Commit:** `style(marketing): put the shared public chrome on signup and legal`
- **Spec:** README §12, envanter A §0 "Re-headed" · **Panolar:** `Signup`, `Legal` ×3

Signup ve legal, M14-14'ün kabuğunu (geçerli sayfa yok) taşır. Gövde **bayt-aynı** kalır. İki önceden var
olan gövde kusuru panolarda düzeltildi, kodda doğrulanır: signup iş türü çipleri Plex Mono 400 (500
değil); cookie tablosu düz ASCII tırnak (`marketing.go`'nun kendi değerleri gibi).

**Dosyalar.** `web/templates/pages/signup.templ`, `web/templates/pages/legal.templ` (yalnız kabuk çağrısı
gerekiyorsa), `web/static/css/input.css`, üretilen `*_templ.go`.

**Kabul kriterleri.** Kabuk kesilince signup ve legal gövdeleri önceki render ile bayt-aynı (ölçüm commit
mesajında); signup testleri (`TestSignup_TheWholeWizardReachesAnApprovedStamp`,
`TestSignupSurface_EverySignInLinkGoesToTheSignInPage` dahil) ve legal testleri yeşil; her-genel-sayfa
testi bu sayfalarda yeşil.

## P2 — Sayfalar

## M14-18 — `/product` (ürün turu)

- **Bağımlılık:** M14-08, M14-14 · **Araç:** yapıcı (`tappa-brand`) + ayrı iddia denetçisi + agent
  `tappa-security-auditor` (§9: 01 "The tap" bölümünün plaket sahnesi burada çizilir; iki telefon M14-15'te
  kendi geçişiyle gelir — bu geçiş sahnenin ve minyatürlerin canlı bileşenleri çağırmadığını ve
  `tap.templ`/`result.templ` diff'inin boş olduğunu doğrular)
- **LP-7:** sayfa, M14-15 (01 sahnesi + "For your team"), M14-16 ("A shift", Rules'tan sonra) ve M14-17
  (Integrations, son bölüm) için yer bırakır; o üç kart bu kartın üstüne gelir.
- **Commit:** `feat(marketing): add the product tour`
- **Spec:** README §14, envanter A §2 (prod-P1–prod-P30) · **Panolar:** `Product` ×3

Hero (H1, lede, iki düğme, "On this page" dizini `#tap #pass #spike #roll #people #venues #rules`) → yedi
bölüm → dürüstlük dipnotu → Integrations (aynı değerler) → CTA → altbilgi. The Pass/Spike/Roll
minyatürleri landing'den **aynen**. People, Venues & plaques ve Rules yeni çizilir, ama yalnız kodun bugün
gösterdiğiyle: çiplerde sayı yok, QR deseni yok, durumlar veritabanı sözcükleriyle, kural cümleleri
`rulebook.go`'dan, etki etiketleri `internal/handler/policies.go`'dan.

**Dosyalar.** yeni `web/templates/pages/product.templ` (+ gerekirse görünüm `.go`),
`web/templates/pages/landingview.go`, `internal/handler/marketing.go` (rota), `web/static/css/input.css`,
`internal/handler/panelsource_test.go` (`nonPanelTemplates`), `internal/handler/marketing_facts_test.go`,
`internal/handler/marketing_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- Rota testi (GET/HEAD 200 ürün host'unda, başka host'ta 404); CSP testi (tam `marketingCSP`, `<script>`
  yok); `Cache-Control: public, max-age=300`.
- prod-P2, prod-P3, prod-P5–prod-P14, prod-P16–prod-P19, prod-P21–prod-P29 ortak iddia kuralıyla (README §18 grupları). Minyatürlerdeki ürün
  dizgeleri (kural cümleleri, etki etiketleri, durum sözcükleri, "Entered by a manager") kaynak sabitlerden
  okunur ve testte karşılaştırılır; KONTROL: bir guardrail cümlesi değişince kırmızı.
- Ortak kontroller (README §11 listesi; her-genel-sayfa testi bu sayfada).

## M14-19 — `/pricing`

- **Bağımlılık:** M14-09, **M14-10** (`main`'in — `0e7c158` — iş dalına alınması: price-R14 `FactAPIEnabledPerBusinessByTaptime`'a dayanır, o Fact yalnız `main`'den gelir), M14-14 · **Araç:** yapıcı (`tappa-brand`) + ayrı iddia denetçisi
- **Commit:** `feat(marketing): add the pricing page with the team-size table`
- **Spec:** README §15, envanter A §3 (price-R1–price-R14) · **Panolar:** `Pricing` ×3

Hero (fiyat başlığı ve lede'si, landing'in fiyat kartı aynen) → Examples (ekip tablosu + donmuş ay fişi
çizimi, "sample figures") → fatura SSS'si (iki sütun `<details>`) → CTA → altbilgi.

**Dosyalar.** yeni `web/templates/pages/pricing.templ`, `web/templates/pages/landingview.go`
(`TeamExample`), `internal/handler/marketing.go` (rota + tablo doldurma), `web/static/css/input.css`,
`internal/handler/panelsource_test.go`, `internal/handler/marketing_test.go`,
`internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- **Ekip tablosu sunucuda hesaplanır:** görünüm `[]TeamExample{Headcount, Monthly}` taşır, her satır
  `exampleTeamMonthly(publishedPricePerEmployeeMonth, n)` ile (10 / 25 / 60 / 150; 60 = `exampleHeadcount`);
  şablonda fiyatın ikinci kopyası yok, JS yok. `TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount`
  (ya da kardeşi) **her satıra** genişler; KONTROL: yayımlanan fiyat değişince her satır değişir, sabit bir
  satır kırmızı olur.
- price-R3–price-R5, price-R8–price-R14 ortak iddia kuralıyla. price-R9 için iki yoldan biri: ya CountLine'ın atlanan şartı da yazılır
  ya da kısaltılmış cümle CountLine'ın özeti olarak açıkça sabitlenir (envanter A price-R9). Ödeme, fatura, KDV,
  API fiyatı **iddia edilmez**.
- Rota ve CSP testleri, ortak kontroller.

## M14-20 — `/security` (sec-S35: LP-7 onayı, Privacy ile aynı commit)

- **Bağımlılık:** M14-07, M14-14 · **Araç:** yapıcı (`tappa-brand`) + ayrı iddia denetçisi + **agent
  `tappa-security-auditor`** (sayfa §4.1, §4.2, §4.3, §4.5, §4.6, §4.7'yi kamuya anlatır; her cümle kodla
  doğrulanır)
- **Kırmızı çizgi:** §4 (anlatım doğruluğu), §7 (sayfa sır taşımaz: anahtar, gerçek IP/koordinat yok — örnek
  ağlar `203.0.113.0/24` belge aralığı)
- **Commit:** `feat(marketing): add the security page and name where the server runs`
- **Spec:** README §16, envanter B sec-S1–sec-S36 · **Panolar:** `Security` ×3

Dört kanıt derinlemesine (bağlantı anatomisi, sayaç satırları, oturum kutuları, /7 genişlik ölçeği, GPS
yarıçapı), karar merdiveni (+ sec-S36 dipnotu), "Flagged, never dropped", değişmezlik, toplanmayanlar, izolasyon.
**sec-S35 — LP-7'de onaylandı (README §20):** yalnız sunucu cümlesi. Başlık "Where Taptime runs.", kart
"Hosted in Germany", cümle "Taptime's production server runs in Falkenstein, Germany." + "Built for GDPR
from day one." (aynen). Yedek/AB-kovası yarısı **yok**. Fact `FactProductionServerInGermany`
(`deploy/README.md`'ye sabit). **Privacy sayfasının bekleyen barındırma olgusu** ("Where the data is hosted
and processed, and the processors involved.", `landingview.go:936`) **aynı commit'te** değişir.

**Dosyalar.** yeni `web/templates/pages/security.templ` (+ görünüm `.go`; SVG geometrisi Go'da),
`web/templates/pages/landingview.go`, `internal/handler/marketing.go`, `web/static/css/input.css`,
`internal/handler/panelsource_test.go`, `internal/handler/marketing_facts_test.go`,
`internal/handler/marketing_test.go`, `web/templates/pages/landingview.go`'nun `LegalPages` Privacy girdisi,
üretilen `*_templ.go`.

**Kabul kriterleri.**
- sec-S1–sec-S36 (sec-S35 dahil) ortak iddia kuralıyla (README §18: kripto/oturum, kurallar ve karar, kayıtlar, değişmezlik,
  gizlilik/izolasyon grupları). Basılan sayılar koddan: debounce ve yarıçap config varsayılanlarından, güven
  puanı motor sabitlerinden, /7 ve /31 `netx` sabitlerinden, "Five of the eleven" `len(policy.Guardrails(…))`'ten,
  merdiven satırları `policy.Guardrails`/`policy.Baseline`'dan. Her biri için **yeni test** (README §19
  tablosu; adlarını yapıcı koyar) + KONTROL (sabit değişince kırmızı).
- sec-S35: `FactProductionServerInGermany` türetmesi `deploy/README.md`'nin düğüm satırından; KONTROL: düğüm adı
  değişince kırmızı. Sayfada "Hetzner", "EU-jurisdiction", "bucket", "backup", "Hosted in the EU" **yok**
  (yeni test).
- **Privacy bağlantısı:** aynı commit'te `LegalPages`'in Privacy bekleyen olgusu güncellenir (sunucu
  cümlesi; işleyenler kısmı beklemede kalabilir). **Yeni test** (adını yapıcı koyar): `/security` sunucunun
  yerini söylerken Privacy barındırmayı hâlâ "bekleyen" sayarsa kırmızı; KONTROL: eski satır geri konunca
  kırmızı. `TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor` yeşil.
- `tappa-security-auditor` ONAY: hiçbir cümle kodun yapmadığını vaat etmez; sır, anahtar biçimi örneği ya da
  gerçek ağ yok.
- Rota ve CSP testleri, ortak kontroller.

## M14-21 — `/for/restaurants`

- **Bağımlılık:** M14-14, M14-20 (paylaşılan Fact'ler: geniş liste, debounce) · **Araç:** yapıcı
  (`tappa-brand`) + ayrı iddia denetçisi
- **Commit:** `feat(marketing): add the restaurants page`
- **Spec:** README §17, envanter B rest-R1–rest-R16 · **Panolar:** `ForRestaurants` ×3

`LandingAudiences` (aynen) üstüne: dokuz mekân karosu, gece vardiyası zaman çizelgesi (Rusty Bar 18:00–02:00,
17:58 → 02:04, "8h 06m · one shift"), mekân tablosu (gerçek `<table>` + `<caption>`), başka mekânda nöbet,
"never invents a check-out", Roll. Başlıkta geçerli öğe yok.

**Dosyalar.** yeni `web/templates/pages/industry.templ` (ya da `restaurants.templ`; M14-22 ile paylaşılır),
`web/templates/pages/landingview.go`, `internal/handler/marketing.go`, `web/static/css/input.css`,
`internal/handler/panelsource_test.go`, `internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.** rest-R1–rest-R16 ortak iddia kuralıyla (rest-R13 `FactNoAutomaticCheckout` bir **yokluk** Fact'idir:
`out` yazan her yol bir dokunuş ya da manuel girişten geçer — `FactNoBulkImport` deseni). Gece çizelgesi
SVG'si Go'da seed vardiyasından hesaplanır; yeni test (bar = Rusty Bar vardiyası, "8h 06m" = defterin süre
biçimi); Roll lejant sözcükleri `reports.templ`'den. Rota ve CSP testleri, ortak kontroller.

## M14-22 — `/for/production`

- **Bağımlılık:** M14-14, M14-20 · **Araç:** yapıcı (`tappa-brand`) + ayrı iddia denetçisi + agent
  `tappa-security-auditor` (§9: **yeni bir telefon varyantı** — "Hello Anna Farrugia" / "KM Plant", 04:02 —
  çizilir; geçiş tap ekranının değişmediğini ve kopyanın canlı bileşeni çağırmadığını doğrular)
- **Commit:** `feat(marketing): add the production page`
- **Spec:** README §17, envanter B fab-P1–fab-P11 · **Panolar:** `ForProduction` ×3

KM Plant kapısında tek plaket, beş departman vardiyası (seed adları), "Same minute, different yardsticks"
başlığı, departman süzgeçli Pass, kapı kuyruğu (aynı kişinin tekrarı IGNORED, yön NONE), eldiven/un, 64 px
düğme, telefon çizimi ("Hello Anna Farrugia", "KM Plant").

**Dosyalar.** M14-21'in sayfa dosyası (paylaşılan şablon) ya da yeni `production.templ`,
`web/templates/pages/landingview.go`, `internal/handler/marketing.go`, `web/static/css/input.css`,
`internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.** fab-P1–fab-P11 ortak iddia kuralıyla (`FactRollCountsLateArrivals`, `FactIgnoredTapIsRecorded`,
`FactPassAndPeopleFilterByDepartment` …). Vardiya barları seed departmanlarından; geç kalma başlığı
`tap.MinutesLate` (04:02 / 04:00 → "0h 02m", raporların etiket biçimi); 60 sn config varsayılanından —
her biri için yeni test + KONTROL. Rota ve CSP testleri, ortak kontroller.

## P4 — LP-7 cilası (kullanıcı, 2026-10-21)

> **Spec:** [README Bölüm III, §20–§28](../design/landing-redesign/README.md) · [lp7-share.md](../design/landing-redesign/lp7-share.md)
> · envanter A'nın LP-7 blokları. Panolar: 26 genel pano (LP-7'de hepsi değişti), `ShareCards`,
> `States-Public`, `M-States-Public`. LP-1…LP-6 kuralları aynen.

## M14-25 — Tek odak halkası + `scroll-padding` (bütün genel sayfalar, Signup ve Legal dahil)

- **Bağımlılık:** M14-14 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `fix(marketing): one focus ring that clears 3:1 on every public surface`
- **Spec:** README §25

**Amaç.** `input.css:1799`'un `.lp :focus-visible { outline: 3px solid var(--saffron); outline-offset: 3px;
border-radius: 4px; }` kuralı (safran porcelain'de **2,32:1**, paper'da 2,62:1 — WCAG 1.4.11'i geçmez) yerine
tek halka: `:focus-visible { outline: 2px solid var(--ring); outline-offset: 2px }`, `--ring` = ink; yeşil ve
ink yüzeylerde (`.ondark` CTA kartı, `.band--ink`) paper. marketingChrome'un (signup, legal, API rehberi)
Tailwind `focus-visible:` sınıfları aynı ink/paper halkaya geçer; safran halka hiçbir genel sayfada kalmaz.
Demo diyaloğunun kapatma düğmesi aynı halkayı alır. **Odak örtülmez (SC 2.4.11):** `html { scroll-padding-top:
72px }` her genişlikte (yapışkan başlık), `scroll-padding-bottom: 80px` 600 px altında (yapışkan çubuk, M14-27).

**Dosyalar.** `web/static/css/input.css`, `web/templates/pages/landing.templ` (yalnız marketingChrome'un
odak sınıfları), `web/templates/pages/signup.templ`/`legal.templ` (yalnız odak sınıfları gerekiyorsa),
`internal/handler/marketing_test.go`, üretilen `*_templ.go`. `layout.*`'a dokunulmaz.

**Kabul kriterleri.**
- **Yeni test** (adını yapıcı koyar): derlenmiş CSS'te genel sayfaların `:focus-visible` kuralları yalnız
  ink ya da paper renkli, 2 px, 2 px offset; safran (ya da 3:1 altı bir renk) hiçbir genel `:focus-visible`
  kuralında yok; `html` `scroll-padding-top` ve 600 px altı `scroll-padding-bottom` taşır. KONTROL: eski
  `.lp :focus-visible` safran kuralı geri konunca kırmızı.
- Odak turu (klavye) landing, beş sayfa, Signup (her adım) ve Legal'de: her odaklanabilir öğede halka görünür
  ve yapışkan başlık/çubuğun altında kalmaz — denetimin "odak sondası, 0 hata" koşulu port edilmiş sayfalarda
  yeniden ölçülür ve sonuç kart notuna yazılır.
- `TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse`, `TestTemplates_CarryNoInlineStyle`,
  `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`, `TestUnbrandedScreens_AreByteIdenticalToTheGolden`
  yeşil/bayt-aynı.

## M14-26 — Landing sekiz bölüm, bant ritmi ve "next step" bağlantıları

- **Bağımlılık:** M14-14, M14-25 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(marketing): cut the landing to eight sections and band the public pages`
- **Spec:** README §21, §22, §23

**Kapsam.**
- Landing sırası: hero → strip → How it works · Why switch · Security · The panel · Built for both shapes ·
  Pricing · FAQ · CTA. `#team`/`#shift`/`#integrations` landing'de **yok** (M14-15/16/17 `/product`'ta).
- Bantlar: hero'dan sonraki her üst düzey bölüm tam genişlik bantta, paper/porcelain sırayla; landing'in Why
  switch'i ink; CTA (`#pilot`) porcelain'de; Signup/Legal bantsız. Paper bantta kart/docket/FAQ öğesi ink/24
  kenar, docket hafif gölge, delik porcelain. Sınıflar `.band--paper`, `.band--porcelain`, `.band--ink`
  şablonda **literal** (sıra sayfa başına sabit).
- `.more`: her bölüm en çok bir "next step" bağlantısıyla biter, tek stil (green, 700, altı çizili, ok).
  Landing: Security → "How each proof works" (`/security`), The panel → "Take the product tour" (`/product`),
  Pricing → "Pricing in detail" (`/pricing`). "Built for both shapes" kartları **bütün-kart bağlantıları**
  (`.fitcard`; `LandingAudiences`'a `Href`), içte etiket; bölüm sonunda ek bağlantı yok. `/pricing`'te yok.
  Her bağlantı hedef rota bağlıyken render edilir.

**Dosyalar.** `web/templates/pages/landing.templ`, `web/templates/pages/landingview.go` (`LandingAudiences.Href`),
sayfa şablonları (bant sınıfları), `web/static/css/input.css`, `internal/handler/marketing_test.go`,
`internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- **Yeni test**: `/` tam bu sekiz bölümü bu sırayla taşır; hiçbir genel sayfa `/#team`, `/#shift`,
  `/#integrations`'a bağlantı vermez (`TestMarketing_EveryInternalLinkResolves` de yeşil). KONTROL: eski
  `#team` bölümü geri konunca kırmızı.
- **Yeni test**: hiçbir genel bölüm birden fazla `.more` taşımaz ve her `.more` bağlı bir rotaya gider;
  KONTROL: bir bölüme ikinci `.more` eklenince kırmızı.
- Bant kuralları yalnız palet token'ları (`TestLanding_UsesOnlyPaletteTokens`); `.band--ink` içinde halka
  paper (M14-25 testi).
- `TestLandingAudiences_EveryClaimRestsOnAProductAnchor`, `TestLandingAudiences_EveryClaimIsActuallyRendered`,
  `TestLandingAudiences_SayNothingAboutTheTwoBreakdownsThatDoNotExist`, `TestLandingSteps_EveryStepIsRenderedInOrder`,
  `TestLandingNav_EveryLinkPointsAtASectionOnThePage` (LP-6 biçimi) yeşil; land-T/S/I iddia testleri
  `/product`'ı okur.

## M14-27 — Telefonda yapışkan "Start free" çubuğu

- **Bağımlılık:** M14-14, M14-25 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `feat(marketing): keep Start free in reach on a phone`
- **Spec:** README §24

600 px altında, ortak altbilgili her genel sayfa (landing, beş sayfa, Legal) sayfa kökünün **son çocuğu**
olarak `.mbar` taşır: "Start free", `position: sticky; bottom: 0`, yalnız CSS, signup rota sabitine giden
≥ 44 px bağlantı, sayı yok. **Signup'ta yok.** 600 px ve üstünde gizli.

**Dosyalar.** `web/templates/pages/landing.templ` (marketingChromeWith + landing kabuğu), sayfa şablonları,
`web/static/css/input.css`, `internal/handler/marketing_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- **Yeni test**: `.mbar` her genel sayfada tam bir kez ve kökün son öğesi, href signup rotası; Signup'ın hiçbir
  adımında yok; sayfada `<script>` eklenmedi (betik kümesi aynı). KONTROL: Signup'a çubuk eklenince kırmızı.
- 390 px ekran görüntüsü: çubuk son denetimi örtmez (`scroll-padding-bottom`, M14-25); altbilginin altında
  durur.
- `TestSignup_MountsExactlyTheRoutesTheLandingPageOffers`, `TestMarketing_EveryInternalLinkResolves` yeşil.

## M14-28 — Paylaşım kartları ve başlık/OG etiketleri

- **Bağımlılık:** M14-14, M14-18…M14-22 (rotalar bağlı) · **Araç:** yapıcı (`tappa-brand`) + ayrı iddia denetçisi
  + agent `tappa-security-auditor` (§7: head etiketlerinde sır, iç host ya da operatör/API host'u yok; `og:url`
  ve `og:image` yalnız `TAPPA_BASE_URL`'den kurulur, istekten — `Host` başlığından — asla; PNG'lerde gerçek
  ad/IP/koordinat yok)
- **Kırmızı çizgi:** §7 (etiketlerde sır yok; mutlak URL yalnız `TAPPA_BASE_URL`'den)
- **Commit:** `feat(marketing): give every public page a title, a description and a share card`
- **Spec:** README §27, [lp7-share.md](../design/landing-redesign/lp7-share.md) · **Pano:** `ShareCards`

**Kapsam.**
- Dokuz PNG (`ShareCards` panosundan, 1200×630) `web/static/og/`'a: `landing`, `product`, `pricing`, `security`,
  `restaurants`, `production`, `signup`, `legal`, `api`; `web/static/og/README.md` her dosyanın boyutu ve
  sha256'sı (fontlar README deseni). Görsellerde fiyat ve ücretsiz ay sayısı yok.
- `documentHead`'e isteğe bağlı OG bloğu (yalnız pazarlama kabukları verir): `<meta name="description">`,
  `og:type=website`, `og:title`, `og:description`, `og:url` (kanonik mutlak), **mutlak** `og:image`
  (`config.BaseURL` + `/static/og/<name>.png`), `og:image:width=1200`, `og:image:height=630`,
  `og:image:alt`, `twitter:card=summary_large_image`. **Sıfır değerli blok bayt üretmez.**
- Başlık ve açıklamalar `lp7-share.md` tablosundan; açıklamalar **mevcut değişkenlere** başvurur (kopya yok);
  tek yeni iddia **api-N1** (rehber açıklaması) → `FactAPIGuideCoversPunchesWritesWebhooks` + türetme
  (`/v1` punch rotası, konum/personel yazmaları, webhook imzalayıcı) + mutasyon kontrolü. Signup açıklamasının
  "3"ü `FreeMonths`'tan. Signup `RobotsPrivate` kalır.

**Dosyalar.** `web/static/og/*.png`, `web/static/og/README.md`, `web/templates/layout/base.templ`
(`documentHead` + pazarlama kabuklarının OG parametresi), `web/templates/pages/*.templ` (çağrılar),
`web/templates/pages/landingview.go`, `internal/handler/marketing.go` (BaseURL'in görünüme geçişi),
`internal/handler/marketing_test.go`, `internal/handler/marketing_facts_test.go`, üretilen `*_templ.go`.

**Kabul kriterleri.**
- **Yeni test** (adını yapıcı koyar): **her genel rota** (`/`, beş sayfa, signup adımları, dört legal sayfası,
  rehberin iki dili) tam bir `<title>`, bir `<meta name="description">`, `og:title`, `og:description`,
  `og:url`, tam **bir** `og:image` (mutlak, `TAPPA_BASE_URL` köklü), `og:image:width/height`, `og:image:alt`
  ve `twitter:card=summary_large_image` taşır; `og:image`'ın gösterdiği dosya **gömülü FS'te var** ve
  1200×630 PNG. KONTROL: bir sayfanın OG bloğu düşürülünce ve bir PNG silinince ayrı ayrı kırmızı.
- CSP değişmez: `img-src` eklenmedi (`TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`,
  `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse` yeşil).
- api-N1 ortak iddia kuralıyla; diğer açıklamalar değişken referansı (yeni test: açıklama dizgesi bir
  `landingview.go` değerine eşit, kopya değil).
- Goldenlar: sıfır değerli OG bloğu bayt üretmediği için `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`
  ve `TestUnbrandedScreens_AreByteIdenticalToTheGolden` bayt-aynı; landing'i OG verisiyle render eden bir
  golden/özet varsa **aynı commit'te** yazılı bayt kontrolüyle yeniden alınır (yalnız eklenen head etiketleri).
  API rehberinin `layout.MarketingTranslated` başlığı için M12-46 bayt notu güncellenir.
- `TestSignup_IsNeverIndexed` yeşil.

## M14-29 — Durum panoları: etkileşim spec'i

- **Bağımlılık:** M14-25, M14-26, M14-27 · **Araç:** yapıcı (`tappa-brand`) · **Kırmızı çizgi:** —
- **Commit:** `style(marketing): give every public control its hover, pressed and current states`
- **Spec:** README §28 · **Panolar:** `States-Public`, `M-States-Public`

Panolar normatiftir: birincil, ghost, başlık ve CTA-bandı düğmeleri; başlık nav bağlantıları (current =
`aria-current`), Sign in, altbilgi ve `.flink` bağlantıları, `.more`; kilit ev bağlantısı, `.fitcard`, `/product`
"On this page" dizini; FAQ `<details>`; telefon Menu özeti, bağlantıları ve açık hali; `.mbar`; demo kapatma
düğmesi; Legal belge değiştirici; Signup metin girdisi, iş türü çipi, yer sayısı kartı, parola alanı ve
"Create my account". Kurallar: **pressed** (`:active`) ink + 2 px green iç kenar (yeşil bantta paper), 1 px
aşağı; **disabled** yalnız form gönder düğmelerinde; **visited** varsayılan rengi korur (`:visited` kuralı yok);
telefonda hover yok; halka M14-25'inki.

**Dosyalar.** `web/static/css/input.css`, gerekirse şablonlarda sınıf ekleri, `internal/handler/marketing_test.go`,
üretilen `*_templ.go`.

**Kabul kriterleri.**
- **Yeni test**: derlenmiş CSS'te panoda sayılan her basılabilir genel sınıf için `:hover`, `:active` ve
  `:focus-visible` kuralı var; genel kapsamda `:visited` kuralı yok; `:disabled` yalnız form gönder düğmelerinde.
  KONTROL: bir sınıfın `:active` kuralı silinince kırmızı.
- Panoya karşı ekran turu (masaüstü + telefon, Menu kapalı/açık): her hücre canlı denetimde eşleşir; farklar
  kart notunda.
- Dokunma hedefleri ≥ 44 px; durum asla yalnız renk değil (current = `aria-current` + biçim).

## P3 — Kapanış

## M14-24 — Faz sonu uzantısı (M14-13 ile tek kapanış)

- **Bağımlılık:** hepsi (M14-25…29 dahil) · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Commit:** M14-13'ünkü (+ yalnız düzeltme commit'leri)

M14-13'ün maddelerine ek olarak:
- kart başına yapılmış `tappa-security-auditor` geçişlerinin kaydı: **M14-04** (§9, hero telefonu ve
  landing.js), **M14-08** (§4.6, vitrin iddiası), **M14-15** (§9, iki telefon), **M14-18** (§9, ürün
  turundaki telefonlar), **M14-20** (§4/§7, `/security` anlatımı), **M14-22** (§9, üretim sayfasının telefon
  varyantı), **M14-28** (§7, head etiketleri ve mutlak URL'ler) — her biri ONAY'lı; artı bütün M14 diff'i
  üzerinde son geçiş;
- beş rota; her genel sayfanın CSP'si (yalnız `/` `script-src` taşır);
- her-genel-sayfa-bağlantıları testi;
- yeni yollar üzerinde ölçülmemiş sayı ve iç ad taramaları;
- README §18'deki her Fact'in türetilmiş + iddia edilmiş + mutasyon kontrollü olması (denetçi
  rastgele beşini bozar);
- sec-S35'in yalnız sunucu cümlesiyle olması ve Privacy'nin bekleyen barındırma olgusunun aynı commit'te
  güncellenmiş olması (LP-7);
- LP-7: landing'de sekiz bölüm ve `/#team`/`/#shift`/`/#integrations`'a bağlantı yok; bant sınıfları;
  bölüm başına en çok bir `.more`; `.mbar` Signup dışında her genel telefon sayfasında; tek odak halkası
  (safran yok) ve `scroll-padding`; paylaşım kartları ve OG etiketleri her genel rotada; durum panolarıyla
  karşılaştırmalı tur (M14-29);
- 29 panonun hepsinin 390 / 834 / 1440 ekran turu (telefonda Menu kapalı ve açık; `ShareCards` dosyalarla,
  `States-Public` ×2 canlı denetimlerle karşılaştırılır);
- `TestEveryNamedTestExists`;
- skill `tappa-brand`'e genel sayfa kabuğunun özeti;
- README Bölüm II–III gerçekleşen sınıf ve rota adlarıyla güncellenir.

**Kabul kriterleri.** Bulgu → düzeltme → yeni denetçi. Bittiğinde `state.md`. `main`'e birleştirme
**kullanıcının deploy kararıdır**.
