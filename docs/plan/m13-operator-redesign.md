# M13 — Operatör yüzeyi yeniden tasarımı: kabuk, gezinme ve ekranların taşınması

> **Durum:** plan (2026-10-20; **OP-15 eki 2026-10-25** — askı yüzeyi, üç orkestratör kararı (a/b/c), atıf
> tazelemesi). Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya
> "yapıldı" yazılmaz.
>
> **Teslim paketi (okumadan başlama):** [docs/design/operator-redesign/README.md](../design/operator-redesign/README.md)
> — kararlar OP-1 (gezinme eklendi, kabuğun tam tarifi) ve OP-2 (üç genişlik), miras kalan D1/D2,
> operatöre özgü kısıtlar (CSP, `Theme{}`, tenant adı bandı, Home okumaz, okuma bütçesi, tasarlanmış
> sızıntı alanları), kabuğu ve işaretlemeyi sabitleyen testler, bileşen kataloğu (Tailwind
> tarifleriyle), ekran haritası. Ekran başına ayrıntı:
> [docs/design/operator-redesign/screens.md](../design/operator-redesign/screens.md) (`file:line`
> atıfları HEAD `022ba3c`'de yazıldı; OP-15 / M15 / legal-links / LP sonrası değişen dosyalardakiler
> 2026-10-25'te `98546448`'e göre yeniden okundu — değişen ve değişmeyen dosyaların ölçülmüş listesi
> README başlığında; bu dosyadaki atıflar da aynı kuralla tazelendi). Panolar: `docs/design/panel-redesign/boards/Op*.dc.html`,
> `T-Op*`, `M-Op*` (12 ekran × 3 genişlik). Ortak token haritası ve D1–D4:
> [docs/design/panel-redesign/README.md](../design/panel-redesign/README.md).

## Kullanıcı kararları (2026-10-20) — bağlayıcı

- **D1 — Kod gerçektir** (paneldeki gibi). Port görünümü taşır, ürün özelliği eklemez; her öğe,
  metin, form ve durum koddan gelir (`screens.md`). Testle sabitlenmiş metin kelimesi kelimesine kalır.
- **D2 — Köşeler.** Adisyon (docket) ve kaşe keskin; kart, alan, düğme yuvarlatılabilir; çipler
  `rounded-full`.
- **OP-1 — Gezinme eklendi** (*"Menü ekle"*). `chrome.templ:19-21`'in "sekme çubuğu yok" kararını
  tersine çevirir. Yalnız bağlantı: yeni okuma, yeni rota, yeni form yok. Bant (`op-bar`, mürekkep +
  4 px safran çizgi, TAPTIME OPERATOR kilidi, Sign out) her genişlikte üstte; masaüstünde **açık**
  (paper) 232 px sol sütun, tablette yatay sekme satırı, telefonda paper alt sekme çubuğu; tek-tenant
  ekranlarında tenant bandı + safran işaretli alt gezinme (Overview · Plaques · Billing · VAT number).
  Panelin sınıfları ve görünümü (`panel-side`, `side-nav`, `bar-link`, `tab-link`, `punchless`, yeşil
  `taptime`, koyu kenar çubuğu) **kullanılmaz** — yalnız kendi `op-*` sınıfları.
- **OP-2 — Kırılma noktaları:** masaüstü (`desk:` ≥ 1200) + tablet (`md:` 768–1199) + telefon
  (< 768). Her ekran kartı üç genişliği **birlikte**, tek DOM ile taşır.
- **OP-15 eki (2026-10-25).** Paket OP-15'ten (tenant askıya alma, ADR 0025; canlı `5aaa562`, goose 63)
  önce yazıldı; panolar askıyı çizmez. D1 gereği kodun askı yüzeyi olduğu gibi taşınır: liste satırının
  **Suspended** çipi (M13-07), genel bakışın **Panel writes** kartı — panosu yok, bileşen kataloğunun
  parçalarından (M13-08), iki audit türü (M13-12), altı problem sayfası (M13-03). Plaques, Billing, VAT
  ve API kartı askıyı okumaz, göstermez. Ayrıntı: README §2 *"OP-15 annex"*, screens.md §4–§6, §10.
  **Orkestratör kararları**:
  - **(a) Çip tonları — kodun bugünkü tonları korunur.** Panoda ton farklıysa pano değil kod kazanır
    (D1): port çipi yuvarlar (`.op-main .tally`), yeniden tonlamaz; tek yeniden tonlama önceden
    kararlaştırılmış `tally--running` kalır. Plaket farkı (README §2 annex (a) tablosu): *On a wall*
    kodda `green-lite` zemin ↔ panoda yeşil %10; *never encoded / not encoded* kodda düz safran çerçeve +
    `saffron-lite` ↔ panoda **kesikli** safran + safran %12; *In stock* kodda `line` çerçeve + `line/10` ↔
    panoda ink %50 çerçeve, kâğıt zemin; *Unrecognised* kodda `ink` + `ink/10` ↔ panoda **kesikli** ink %50,
    kâğıt; *Retired / Lost* aynı. Aynı pano paleti OpTenant (API Open/Closed) ve OpVat'ta da var; orada
    da kod kazanır (M13-02 zaten `tally--running` dışındaki her ton kuralını korur).
  - **(b) `aria-current`:** ana gezinmede (rail — masaüstü sütunu / tablet satırı / telefon çubuğu, tek
    DOM) geçerli öğe `aria-current="true"`; tek-tenant alt gezinmesinde (Overview · Plaques · Billing ·
    VAT number) geçerli sekme `aria-current="page"`. **Gerekçe:** rail sayfanın **bölümünü**, alt sekme
    **sayfayı** işaret eder — Tenants listesinden açılmış bir tenant'ın Plaques sayfasında rail "Tenants"
    bölümündedir ama o sayfa değildir; rail'de `page` ekran okuyucuya "geçerli sayfa: Tenants" dedirtir
    ve tek-tenant sayfasında iki öğe birden "sayfa" olurdu. Rail her ekranda `"true"` taşır (öğenin
    bağlantısı sayfanın kendisi olsa da — Home'da Console, listede Tenants), böylece her nav'da tek
    anlam, kabuk testinde nav başına tek değer. İmzalı problem sayfasında ikisi de yok. Bağlandığı yerler:
    README §2 kabuk maddeleri, §4.1, §6 "Main nav" / "Tenant sub-nav", screens.md §0, M13-01.
  - **(c) Genel bakışın `<h1>`'i sayfa başlığıdır, tenant adı değil:** `<p class="docket-label">Tenant
    overview</p>` → `<h1 class="docket-label">Tenant overview</h1>` (yalnız öğe); tenant adı bandı `<p>`
    kalır. Ölçüm, gerekçe ve test tarifi M13-08'de; README §2 annex (c), screens.md §6.

**Yeni okuma kararı gerekmiyor.** Panoların her öğesi bugünkü okumalardan gelir (envanter + 12 pano,
2026-10-20). Bir öğe yeni okuma isterse (ör. Home'a bir sayı) bu bir **karar**dır, kart değil: yeni
`op_*` = migration + ADR 0021 + kullanıcı onayı (CLAUDE.md §4.5 istisna (1)). Bu planda böyle bir
karar yok; "çizilmiş ama yapılmamış" listesi de yok.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı**
  bir denetçi kabul kriterlerini tek tek doğrular. Bulgu varsa düzeltilir ve **yeni** bir denetçiyle
  yeniden denetlenir; onay gelmeden kart `done` olmaz ve sonrakine geçilmez. §4'e değen kartlarda
  (kabuk, tenant ekranları, holding, sızıntı yüzeyi) ayrıca agent `tappa-security-auditor`. Her UI işi
  skill `tappa-brand` okunarak.
- **Testler:** tur içinde yalnız **değişen** testler `-run` ile (operatör paketi yavaştır — backlog
  T101; `go test ./internal/handler/operator -run 'OperatorScreens|EnrollQR'` gibi) +
  saniyelik kapılar (`gofmt`, build, `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...` ve
  `make check` bir kartta **bir kez**, commit'ten önceki son doğrulamada. Yapıcı ve denetçi brief'ine
  bu yazılır.
- **`make gen` + `make css`** her `.templ` / `input.css` değişikliğinden sonra; üretilen dosyalar
  commit edilir.
- **Sabitlenmiş işaretleme** (README §4) değişirse test **aynı commit'te**, iddianın niyeti korunarak
  güncellenir; hiçbir iddia gevşetilmez. Henüz var olmayan bir testin adı belgeye yazılmaz
  (`TestEveryNamedTestExists`) — yeni testi tarif et, adını yapıcı koyar.
- **Golden politikası:** operatör yüzeyinin HTML golden'ı **yoktur** (işaretleme testlerle
  sabitlenir, README §4). `TestUnbrandedScreens_AreByteIdenticalToTheGolden` (tap/aktivasyon
  ekranları; `components.Notice`, `layout.*`) ve marka CSS golden'ı (`TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`)
  M13 boyunca **bayt-aynı** kalır; bunlar hiçbir kartta yeniden üretilmez — kırmızıya dönerlerse
  kart paylaşılan bir bileşene dokunmuştur ve düzeltilecek olan koddur, golden değil.
- **Dal:** `m13-operator` (worktree `../tappa-m13`). `main`'e birleştirme bir deploy kararıdır —
  kullanıcı istemedikçe yok.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu.

## Her kartın miras aldığı kısıtlar (özet — tam metin README §3)

CSP `default-src 'none'; style-src 'self'; font-src 'self'; form-action 'self'` — **`img-src` yok**:
`<img>`, logo, CSS arka plan görseli, `data:` URI yok; ikonlar ve QR satır içi SVG · yeni JS yok (tek
betik `enroll.js`, yalnız kayıt sayfasında) · `Theme{}`: tenant markası, accent, tema bağlantısı yok;
adında `brand` geçen sınıf yok · tek-tenant ekranında tenant **adı** bandı zorunlu (ADR 0020 §9) ·
Home okumaz → sayı/widget yok · her veri ekranı görüntülemesi bir `read` satırı + bir bütçe birimi;
birleşik ekran, otomatik yenileme yok · `operatorpages` yalnız `components` ve `layout`'u import eder;
`components.Notice` ve `layout.*` değişmez; yeni bileşenler `operatorpages` içinde **dışa kapalı**
(`TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` dışa açık bileşenleri 14 ekrana
sabitler) · tasarlanmış sızıntı alanları dışında hiçbir sır/terim/VAT/holding adı/operatör e-postası
yok · arama, denetim süzgeci ve fatura sayfalayıcısı POST gövdesi kalır · yalnız aynı kökenli,
bağlanmış yollar · §4.6: ölçülmemiş şey için 0 yok · IBM Plex Mono yalnız 400/700 · dokunma hedefi
≥ 44 px (`min-h-11`) · Tailwind yalnız `.templ` ve `web/static/js`'i tarar; sınıf adı Go'da kurulmaz,
templ yorumuna yardımcı sınıf adı yazılmaz · tek `<h1>`.

## Görev listesi

| ID | Faz | Görev | Ajan | Bağımlılık |
|---|---|---|---|---|
| M13-01 | O0 | Operatör kabuğu + gezinme (OP-1): bant, `op-nav` (üç genişlik), tenant alt gezinmesi, içerik sütunu; kabuk testinin genişletilmesi | yapıcı (`tappa-brand`) + güvenlik | — |
| M13-02 | O1 | Bileşen katmanı: `op-card`, `op-input`, düğmeler, `op-row`, kapsamlı yuvarlak çip, `op-label` | yapıcı (`tappa-brand`) | M13-01 (M12-52: ad değişikliği canlıda; kalan KONTROL bu karta katıldı) |
| M13-03 | O1 | Operatöre özgü bildirim (`opNotice`) + problem sayfası | yapıcı (`tappa-brand`) | M13-02 |
| M13-04 | O2 | Ekran: giriş + kod (OpSignIn) | yapıcı (`tappa-brand`) | M13-03 |
| M13-05 | O2 | Ekran: hesap kurulumu (OpEnroll) | yapıcı (`tappa-brand`) + güvenlik | M13-03 |
| M13-06 | O2 | Ekran: konsol (OpHome) | yapıcı (`tappa-brand`) | M13-02 |
| M13-07 | O2 | Ekran: tenant listesi ve arama (OpTenants) | yapıcı (`tappa-brand`) + güvenlik | M13-02 |
| M13-08 | O2 | Ekran: tenant özeti + entegrasyon API'si (OpTenant) | yapıcı (`tappa-brand`) + güvenlik | M13-03 |
| M13-09 | O2 | Ekran: plaket envanteri (OpPlaques) | yapıcı (`tappa-brand`) | M13-02 |
| M13-10 | O2 | Ekran: fatura ayları (OpBilling) | yapıcı (`tappa-brand`) | M13-02 |
| M13-11 | O2 | Ekran: VAT yeniden denetimi (OpVat) | yapıcı (`tappa-brand`) + güvenlik | M13-03 |
| M13-12 | O2 | Ekran: denetim günlüğü (OpAudit) | yapıcı (`tappa-brand`) + güvenlik | M13-02 |
| M13-13 | O2 | Ekran: holding'ler (OpHoldings) | yapıcı (`tappa-brand`) + güvenlik | M13-03 |
| M13-14 | O2 | Ekran: yasal metinler (OpLegal) | yapıcı (`tappa-brand`) | M13-03 |
| M13-15 | O3 | Son tam `make check` + `tappa-security-auditor` (tenant ötesi yüzey, §4.5 istisna (1)) + skill/ADR toparlaması | denetçi + güvenlik | hepsi |

O2 kartları birbirinden bağımsızdır; her biri kendi bağımlılıkları bitince tek başına sevk edilebilir
(dal üzerinde, CI yeşil). Her ekran kartı üç genişliği birlikte taşır.

---

## O0 — Kabuk

## M13-01 — Operatör kabuğu ve gezinme (OP-1)

- **Bağımlılık:** —
- **Kırmızı çizgi:** §4.5 istisna (1) (yüzey tenant ötesidir: kabuk okuma yapmaz, tenant adını yalnız
  bantta taşır), §9 (tenant markası bu yüzeyde yok), ADR 0020 §9
- **Araç:** yapıcı, skill `tappa-brand` + agent `tappa-security-auditor`
- **Commit:** `feat(operator): add the operator navigation and widen the operator shell`
- **Spec:** [README §2 OP-1, §4.1, §6](../design/operator-redesign/README.md), [screens.md §0](../design/operator-redesign/screens.md)

**Amaç.** Bütün imzalı operatör ekranlarına OP-1'in kabuğunu getirmek: değişmeyen bant, tek DOM'lu
`<nav class="op-nav" aria-label="Operator sections">` (masaüstü sol sütun / tablet sekme satırı /
telefon alt çubuğu), tek-tenant ekranlarında tenant bandının **içinde**, adın altında
`<nav class="op-subnav" aria-label="This tenant">`, ~960 px içerik sütunu
(`<main class="op-main …">`).

**Neden.** Kullanıcı kararı OP-1. Bugün `/operator` menünün kendisi; her ekran yalnız "Back to …"
bağlantıları taşıyor (`chrome.templ:19-21`). Bütün ekran kartları bu kabuğun üstüne oturur.

**Kapsam.**
- `chrome.templ`: `bar` işaretlemesi bayt-aynı (iç satır artık tam genişlik); `screen(title,
  signedIn)` → geçerli bölümü de alır (ör. `screen(title, signedIn, current navItem)`); imzasızda nav
  yok, imzalı problemde nav var ama geçerli öğe yok; `scriptedScreen` (Enroll) nav taşımaz.
- `tenantScreen` / dışa açık `TenantScreen`: alt gezinme için dört yolu ve geçerli sekmeyi alır (ör.
  bir `TenantTabs{Overview, Plaques, Billing, VAT string; Current}` değeri); yollar handler'da kurulur
  (görünümlerin `OverviewPath`/`PlaquesPath`/`BillingPath`/`VATPath` deseni). `TenantScreen` dışa açık
  kalır (14 ekran listesi değişmez); `opNav`, `opSubNav` dışa kapalı. Adsız tenant reddi aynen
  (`TestTenantScreen_RefusesToRenderWithoutAName`).
- Nav: beş bağlantı, sıra Console · Tenants · Holdings · Audit log · Legal texts; telefonda
  "Audit"/"Legal" kısa etiketleri (`md:hidden` / `hidden md:inline` span çifti); beş ikon README §2'deki
  yollarla satır içi `<svg … aria-hidden="true">`, yalnız telefonda görünür; masaüstünde "PLATFORM"
  etiketi; geçerli öğede `aria-current="true"` (OP-15 eki karar (b): rail **bölümü** işaret eder), tek
  öğede. Alt gezinmenin geçerli sekmesi `aria-current="page"` (sayfa).
- Sign out tek form ve tek **metin** düğmesi kalır — her genişlikte aynı; telefonda ikon/`<svg>` yok
  (plaket sayfası tam bir `<button>` ister, imzalı sayfanın SVG'leri yalnız nav'ın beşi).
- **DOM sırası** (README §2): `header.op-bar` → `div.op-frame` → `nav.op-nav` → `div.op-col` →
  [`section.op-tenant` (ad + içinde `nav.op-subnav`)] → `main.op-main`. Bant ve alt gezinme
  `<main>`'in **dışında ve önünde** (bugünkü `tenantScreen` sırası); `<header class="op-bar">` yine
  `<main`'den önce.
- İçerik sütunu: `div.op-col` + `<main class="op-main flex w-full max-w-[960px] …">` — **sola dayalı
  (pano)**, `mx-auto` yok; tenant bandının iç satırı da sola dayalı (README §6 "Content column",
  "Tenant banner"; orkestratör kararı 2026-10-25: pano kazanır); **`op-main` sınıfı bu kartta konur** — M13-02'nin çip kuralı ona
  bağlıdır. İmzasız ekranların `<main>`'i de `op-main` taşır.
- `input.css`: `.op-frame`, `.op-nav`, `.op-nav-item`, `.op-nav-label`, `.op-subnav`, `.op-subnav-item`
  (README §6 tarifleri; tablette `md:static md:order-first md:mt-0 md:border-t-0 md:pb-0`, öğe
  `md:flex-row md:min-h-11` — telefonun sticky alt çubuğu tablette üstte, akışta bir satırdır), basılabilir olanlarda `min-h-11`; `.op-tenant`
  çizgisi **1 px** kalır, iç alt dolgusu kalkar (alt gezinme çizginin üstüne oturur); alt gezinme
  telefonda 14 px ve satır kırar.
- **Telefon alt çubuğu (cilalı panolar, 2026-10-20):** `fixed` değil **`sticky bottom-0`**; kısa
  sayfada da görüntü alanının alt kenarına oturur: `.op-frame` en az görüntü alanı yüksekliğinde
  (bant hariç: telefonda `min-h-[calc(100vh-3.75rem)]`, tabletten `md:min-h-[calc(100vh-4.25rem)]` — panonun 56 / 64 px bant satırı + 4 px çizgi; orkestratör kararı 2026-10-25: pano kazanır) bir **flex sütun**dur, nav `order-last mt-auto`
  alır (tablette `md:static md:order-first md:mt-0`, masaüstünde `desk:flex-row`,
  `desk:order-none`). `layout.Operator`'ın `<body>`'sine dokunulmaz (paylaşılan kabuk). Çerçevede
  ya da nav'ın hiçbir atasında `overflow-*` yok (sticky'yi bozar). Sticky çubuk akışta yer
  tuttuğundan son denetimi örtmez; `<main>`'in eski `pb-24` boşluğu gerekmez (README §6
  "Frame", "Main nav", "Content column").
- Ekran gövdeleri bu kartta **değişmez** (yalnız sarmalayan kabuk); her ekranın kendi "Back to …"
  bağlantıları kalır.
- ADR 0020'ye (§4 ya da §9 yanına) ve ADR 0023'ün operatör satırına tarihli not: OP-1 gezinmesi
  eklendi, marka kuralı değişmedi (hâlâ *TAPTIME OPERATOR* kabuğu, tenant markası yok);
  `chrome.templ`'in "No tab bar" yorumu yeni gerekçeyle yeniden yazılır.

**Dosyalar.** `web/templates/operatorpages/chrome.templ`, `view.go` (`TenantScreen` imzası, sekme
tipi), her ekranın `@screen` / `@TenantScreen` çağrısı (`home.templ`, `signin.templ`, `tenants.templ`,
`plaques.templ`, `billing.templ`, `vat.templ`, `audit.templ`, `holdings.templ`, `legal.templ`),
`internal/handler/operator/{tenants,plaques,billing,vat}.go` (sekme yolları), `web/static/css/input.css`,
`internal/handler/operator/op8_test.go` (`screens()`, kabuk testi), `enrollqr_test.go`, `op14_test.go`
(44 px listesi), `docs/adr/0020-…md`, `docs/adr/0023-…md`.

**Kabul kriterleri.**
- `TestOperatorScreens_EveryOneWearsTheOperatorChrome`'un (`op8_test.go:784-821`) **beş mevcut iddiası
  bayt-aynı** kalır (`<header class="op-bar">` `<main`'den önce, tam kilit, panel işaretleri yok,
  sign-out tam 40 imzalı render'da, başlık `Taptime operator</title>` ile biter) ve şunlarla
  **genişletilir**: imzalı 40 render'da tam bir `<nav class="op-nav"` bant ile `<main` arasında, tam
  beş href (`/operator`, `/operator/tenants`, `/operator/holdings`, `/operator/audit`,
  `/operator/legal`); imzasız 9 render'da hiç; ana nav'da `aria-current="true"` "problem, signed in"
  dışındaki 39 imzalı render'da tam bir ve doğru öğede (Home → Console; liste + dört tek-tenant ekranı
  → Tenants; Holdings, Audit log, Legal texts kendilerinde), imzalı problemde sıfır; `op-nav` içinde
  hiçbir render'da `aria-current="page"` yok; **her tek-tenant render'ında (19** — 18 overview /
  plaques / billing / VAT fixture'ı + çıplak "tenant screen" fixture'ı**)** alt gezinme bandın
  **içinde** (`<section class="op-tenant"` … `<nav class="op-subnav"` … `</section>`, `<main`'den
  önce), tenant'ın dört yolu ve tam bir `aria-current="page"`; başka hiçbir render'da alt gezinme yok.
  (Sayılar `022ba3c`'de 46 / 37 / 36 / 17 idi; OP-15 üç imzalı fixture ekledi — "tenants, a suspended
  row", "tenant overview, suspended", "tenant overview, an unrecognised reason"; `op8_test.go:736,
  742-743`, testin PREMISE'i 49 / 40.)
- **Yeni test** (adını yapıcı koyar; kabuk testinin yanında): hiçbir operatör render'ı panel kabuğunun
  işaretlerini (`panel-side`, `side-nav`, `bar-link`, `tab-link`, `sheet-`, `panel-well`,
  `panel-stripe`, `punchless`, yeşil `taptime`) ya da tenant markasını (`/brand/` — tema ve logo yolları,
  `bg-brand`, `--brand`, `<img`) taşımaz; KONTROL: aynı tarayıcı bir panel render'ında en az birini
  bulur (gözü açık olduğu ölçülür). Çıplak "brand"/"accent" sözcüğü aranmaz (zayıf parola bildirimi
  "a letter with an accent" der).
- `TestEnrollQR_OnlyOnTheFirstLoad`'ın SVG sayımı **genişletilir**, gevşetilmez: kayıt ilk yüklemesi
  tam 1 (QR), öteki imzasız render'lar 0, imzalı render'lar tam 5 ve beşi de `aria-hidden="true"` ve
  `op-nav`'ın içinde.
- `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` yeşil (nav ve alt gezinme yolları bağlı
  rotalar) ve KONTROL'üne nav'ın beş yolu ile alt gezinmenin dört yolu eklenir; form sayıları değişmez.
  Bu test **açılış etiketi öneklerini, öznitelik sırası dahil** eşler (ör. `<form method="post"
  action="/operator/holdings" class=`, `<form method="post" action="/operator/tenants/<id>/billing">`) —
  `method` `action`'dan önce, sondaki `class=` bugünkü gibi var ya da yok.
- Plaket sayfası: tam **bir** `<form` (sign-out), tam **bir** `<button`, hiç `hx-` yok
  (`op13_test.go:327-332`) — nav ve alt gezinme form/düğme taşımaz.
- `TestOperatorChrome_TheColouredTextClearsAA` genişletilir: nav'ın `ink/70` etiketi paper'da ve
  geçerli öğenin `ink`'i `ink/5`-üstü-paper'da ≥ 4.5:1; alt gezinmenin `ink`'i paper'da.
- `op14_test.go:753`'teki input.css 44 px listesine yeni basılabilir `op-*` sınıfları eklenir.
- Okuma sayısı değişmez: `TestReadBudget_TheReadsOfEveryScreenShareOneBudgetPerSession`,
  `TestHoldingsBudget_AViewIsOneReadAndAWriteIsNone`, `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`,
  `TestOperatorHeaders_FortyResponseClassesCarryThePolicy`, `TestOperatorPages_ImportedOnlyByTheSurfaceAndSharingOnlyTheShell`,
  `TestOperatorPages_TheExportedScreensAreTheOnesScreensRenders` yeşil.
- `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`, `TestUnbrandedScreens_AreByteIdenticalToTheGolden`
  yeşil (`layout.*` ve `components.*` değişmedi).
- 390 / 834 / 1200 / 1440'ta yatay sayfa kaydırması yok; telefonda alt çubuk son denetimi örtmez
  ve kısa bir sayfada (ör. Home, imzalı problem) görüntü alanının alt kenarında durur.

**Tuzaklar.** Telefon çubuğunu `fixed` yapmak (kısa sayfada da çalışır ama içerik üstüne biner ve
`pb-24` ister — panolar sticky + `mt-auto` çizer) · çerçeveye `overflow-hidden` vermek (sticky
çalışmaz) · Nav'ı üç ayrı DOM olarak yazmak (üç kat bağlantı, üç `aria-current`) · sign-out'u
telefon için ikinci bir forma kopyalamak (form sayımları kırılır) · panelin `.panel-side`/`.bar-link`
sınıflarını "yeniden kullanmak" · ikonları CSS arka planı yapmak (CSP'de `img-src` yok) · kayıt
sayfasına nav koymak (SVG sayımı 1 olmalı; kayıt imzasızdır) · `TenantScreen`'i dışa kapalı yapmak ya
da yeni dışa açık bileşen eklemek · bant işaretlemesine dokunmak · alt gezinmeyi bandın dışına, paper
zemine koymak (panolar bandın içinde çizer) · telefonda sign-out'u ikona çevirmek.

---

## O1 — Paylaşılan operatör bileşenleri

## M13-02 — Bileşen katmanı

- **Bağımlılık:** M13-01. **M12-52'nin durumu (ölçüldü 2026-10-25):** M12 canlıda (`ab85193`,
  2026-10-21 — state.md) ve `origin/main` `5524505` + `00ca1e6`'yı içerir: `.tally--pill` → `.pill-tally`
  ad değişikliği kodda (`input.css:1307` @ `98546448`) ve `TestTallyRules_NoToneRuleColoursTheWord`
  yeşil (bu tarihte `-run` ile koşuldu). M12-52'nin **kalan** kabulü — bir `.tally--x` kuralına eklenen
  **biçim** yardımcısının (ör. `rounded-full`) bulgu olduğunu gösteren ikinci mutasyon KONTROL'ü —
  testte **yok**: `op12_test.go` OP-12'den (`a54459d`) beri değişmedi ve okuyucunun tek mutasyonu
  `text-saffron`'dur (`op12_test.go:1634-1640`). M12 kapandığı ve bu kart `readTallyRules`'a zaten
  dokunduğu için o KONTROL **bu kartın kabulüne katıldı** (aşağıda); M12-52 artık bu kartı bloke etmez
  ([m12-panel-redesign.md → M12-52](m12-panel-redesign.md))
- **Kırmızı çizgi:** — (yalnız sınıflar; D2)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `style(operator): round the operator cards, fields and chips as drawn`
- **Spec:** [README §5–§6](../design/operator-redesign/README.md)

**Amaç.** README §6'nın operatör sınıflarını `input.css`'in `op-*` bloğunda (`input.css:1114-1161` @
`98546448`) güncellemek: `.op-card` `rounded-[14px]`, `shadow-sm` düşer (hiçbir operatör kartında gölge yok), iç boşluklar üç genişlikte
(telefonda **20 px**, `p-5`; tablet/masaüstünde 22 px 24 px); docket dolgusu kapsamlı kuralla
`.op-main .docket { @apply px-5 py-4 md:px-6 md:py-[18px]; }` — telefonda 16 px 20 px,
tablet/masaüstünde 18 px 24 px; `.docket`'in kendi `px-4 py-3`'ü panel için kalır. **Tek ölçek, tek
görünüm:** her operatör ekranında delikli `.docket` (panoların Tenants/Tenant/Plaques/VAT'taki kesikli-üst
kısayolu da delikli docket'e hizalanıyor) ve her kart/docket için bu tek dolgu ölçeği — ekran başına
dolgu yok. **Kart içi boşluk** (README §6 "Card": `flex flex-col gap-3.5`) bu kartta **yapılmaz**
(2026-10-25, denetim): `.op-card` her ekranda paylaşılır ve bugünkü gövdeler kendi `mt-*`'lerini
taşır — kurala girerse taşınmamış her ekranın boşluğu ikiye katlanır. Her ekran kartı kendi
ekranının kartlarında yapar (aşağıda her ekran kartında tek satır), M13-15 üç yardımcıyı `.op-card`'a
taşır; `.op-input`
`rounded-[10px]`; `.op-signout` ve operatör ekranlarındaki `btn` `rounded-[10px]`; `.op-row`/`.op-version`
kesikli ayraç; yeni `.op-label` (mono 11 px etiket); çipler `rounded-full` — **span'e sınıf eklemeden**,
kapsamlı kural `.op-main .tally { @apply rounded-full px-2.5 py-0.5 font-bold leading-[18px] tracking-[0.08em]; }`
(`op-main` sınıfını M13-01 `<main>`'e koydu; 18 px satır + 2 px + 2 px + 1 px çerçeve = panoların 24 px'i).

Cilalı panoların getirdiği yeni sınıflar (yalnız CSS; işaretlemeye ekran kartlarında girer):
- **`.op-pager`** — Billing / Audit / Holdings sayfalayıcısının çerçeveli düğmesi (panoların
  `.pgbtn`'i; README §6 "Pager"): `inline-flex min-h-11 items-center justify-center rounded-[10px]
  border border-ink/30 bg-paper px-[22px] font-display text-[15px] font-bold text-ink no-underline
  hover:border-ink`. Testlerdeki sınıf değişikliği ekran
  kartlarında (M13-10, M13-12, M13-13), işaretlemeyle aynı commit'te.
- **`.op-step`** (Enroll adımları, 16/17 px) ve **`.op-heading`** ("Create a holding", 19 px) —
  bugün `docket-label` yazılan kart içi başlıklar (README §6).
- **`.op-receipt`** — fatura satırının dört rakamlık fiş şeridi (README §6 "Billing receipt strip").
- **Odak halkası** (M13-01 denetiminden, 2026-10-25) — `.op-nav-item`, `.op-subnav-item` ve `.op-link`
  için `focus-visible` ink halka, panel nav'ıyla tutarlı (`focus:outline-none` yerine
  `focus-visible:` görünür bir ink çerçeve); bugün tarayıcı varsayılanı görünür (WCAG 2.4.7
  sağlanıyor), bu madde tutarlılık içindir. Halka rengi ink (ink on paper 16.17:1; saffron-lite
  bant üstünde de ink).
- **Satır içi bağlantı isabet alanı** — denetim satırının tenant bağlantısı için kapsamlı kural,
  tablet/masaüstünde satır yüksekliğini korur ve 44 px görünmez bir `::before` örtüsü ekler
  (README §6 "Inline link hit area"); işaretleme değişmez. Bağlantı `md:inline-block`'tur, `inline`
  değil (denetim, ölçüldü): satır kıran uzun bir tenant adında satır içi bağlantı iki parçaya bölünür
  ve örtü 18 px'e çöker; panodan farkı README'de.
- **`tally--running` yeniden tonlanır, yeni değiştirici yok:** canlı ay kesikli safran çerçeve —
  seçicisi çizgi grubundan (`input.css:745-750`) çıkar, kendi tek satırlık kuralını alır:
  `.tally--running { @apply border-dashed border-saffron bg-paper; }` (panoların `.t-saffron-o`'su: kâğıt zeminde kesikli safran çerçeve). Ad `tally--[a-z-]+`'e
  uyar, çip regex'i değişmez.

**Dosyalar.** `web/static/css/input.css` (`op-*` bloğu); gerekirse `web/templates/operatorpages/*.templ`
yalnız `op-label` için; `internal/handler/operator/op12_test.go` (çip okuyucusu ve fatura ton
testleri), `op14_test.go` (44 px listesi).

**Kabul kriterleri.**
- `.docket` keskin kalır (D2); `.tally` taban kuralı ve `tally--running` **dışındaki** her ton kuralı
  olduğu gibi kalır (panel de kullanır); `tally--running` yalnız `billing.templ:104`'te yazılır ve
  aşağıdaki gibi yeniden tonlanır; yuvarlama yalnız operatör `<main>`'i altında.
- Çip regex'i `<span class="tally (tally--[a-z-]+)">` her ekranda eşleşir; `TestTallyRules_NoToneRuleColoursTheWord`,
  `TestCompiledCSS_TallyWordIsInkOnEveryChip`, her `Test…Screen_EachChipHasTheRuleTheContrastTestComputes` yeşil.
- `TestTallyRules_NoToneRuleColoursTheWord`'ün okuyucusu `readTallyRules` (`op12_test.go:1645-1707`)
  `.tally` geçen her seçiciyi okur ve yalnız taban `.tally` ile `.tally--x` listelerine izin verir —
  `.op-main .tally` bugün bir bulgudur. Okuyucu **tek bir** yeni biçimle genişletilir: seçicisi
  **tam olarak** `.op-main .tally` olan, tek `@apply`'lı kural, yalnız biçim ve yazı yardımcıları (`rounded-*`, `px-*`, `py-*`, `min-h-*`, `leading-*` (yalnız `line-height` üretir — ölçüldü; çipin 24 px'i ondan: `leading-[18px]`),
  `font-bold`, `tracking-*` — panoların çipi 24 px, 2 px 10 px, kalın, .08em; `text-*`, `bg-*`,
  `border-<renk>`, `opacity` yok); KONTROL: kapsamlı kurala bir renk yardımcısı eklenmiş mutasyon
  kırmızı. **Bu kart, bir `.tally--x` kuralının uygulayabileceğini genişleterek asla geçilmez** —
  okuyucunun `border-`/`bg-` dışını bulgu sayan kolu aynen kalır. (Bu test panelin `.tally--pill`'i — `2afa9db` — yüzünden kırmızıydı; M12-52'nin ad değişikliği
  `5524505` + `00ca1e6` (`.pill-tally`, `input.css:1307` @ `98546448`) onu yeşile döndürdü — ölçüldü,
  2026-10-25'te yeniden.)
- **M12-52'den devralınan KONTROL:** `TestTallyRules_NoToneRuleColoursTheWord`'e, bir `.tally--x`
  kuralına eklenmiş bir **biçim** yardımcısının (ör. `rounded-full`) bulgu olduğunu gösteren mutasyon
  eklenir — mevcut `text-saffron` mutasyonunun biçimiyle (`op12_test.go:1634-1640`: değiştirilmiş CSS'i
  okuyucuya ver, bulgu yoksa kırmızı). Böylece kapsamlı `.op-main .tally` istisnası bir ton kuralına
  biçim sızdırmanın yolu olamaz.
- `tally--running`'in yeni tonu iki fatura testinde **aynı commit'te** genişletilir, gevşetilmez:
  `TestBillingScreen_EachChipHasTheRuleTheContrastTestComputes` (`op12_test.go:1532-1583`; `want`
  `BillingToneRunning: {"bg-paper", "border-saffron"}` olur ve yalnız bu ton için üçüncü
  belirteç olarak tam `border-dashed`'e izin verilir — başka her ek belirteç yine kırmızı;
  KONTROL: `border-dashed`'siz canlı kural da kırmızı, yani kesikli çizgi ölçülür) ve
  `TestBillingScreen_TheChipsAndTextClearAA` (`op12_test.go:1471`; canlı ay mevcut "ink on paper" satırına adıyla taşınır — 16.17:1 —,
  `line`/10 satırı yalnız before sign-up için kalır).
  `readTallyRules` `border-dashed`'i zaten kabul eder (`border-` öneki).
- `.op-input` / `.op-link` hâlâ `min-h-11` (`op12_test.go:1457`, `op14_test.go:753`); `.op-pager`
  `op14_test.go:753`'teki listeye eklenir.
- Üretilen işaretleme ekran gövdelerinde değişmez (yalnız CSS) — kabuk testi ve docket anatomisi
  testleri dokunulmadan yeşil (yeni sınıflar işaretlemeye ekran kartlarında girer).

## M13-03 — Operatöre özgü bildirim ve problem sayfası

- **Bağımlılık:** M13-02
- **Kırmızı çizgi:** §9 (`components.Notice` tap/aktivasyonla paylaşılır, değişmez); güvenlik başlıkları
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(operator): give the operator surface its own notice and problem card`
- **Spec:** [README §6 "Operator notice", "Problem page"](../design/operator-redesign/README.md), [screens.md §4](../design/operator-redesign/screens.md)

**Amaç.** `operatorpages/notice.templ`'de dışa kapalı `opNotice(tone, heading)` (ok / warn / alert;
`role="status"` / `role="alert"`); bütün operatör çağıranları (`signin.templ`, `enroll.templ`,
`holdings.templ:70-88`, `tenants.templ:230-248` (API) ve `:339-343` (askı kartının security-hold
uyarısı, OP-15), `vat.templ:35`, `legal.templ:56`, `home.templ:55`)
ona geçer — **ton ve metinler değişmeden**. `Problem` (`home.templ:53-65`) yeni çerçevede: imzasız
ortalanmış kart, imzalı nav'lı ve geçerli öğesiz; durum çipi yok (görünüm durumu taşımaz — D1).

**Dosyalar.** `web/templates/operatorpages/notice.templ` (yeni), `signin.templ`, `enroll.templ`,
`holdings.templ`, `tenants.templ`, `vat.templ`, `legal.templ`, `home.templ`, `web/static/css/input.css`;
`internal/handler/operator/op10_test.go` (`warnNotice`), `holdings_test.go` (`headingsOn`),
`tenantapi_test.go` (`apiHeadingsOn`) — yalnız başlık biçimi değişirse; `op15_test.go`
(`TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice`'ın `</section>` kesiti) — yalnız `opNotice`'in
kökü `<section>` değilse.

**Kabul kriterleri.**
- `components.Notice` baytları değişmez (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`);
  `operatorpages` hâlâ yalnız `components` ve `layout`'u import eder; `opNotice` dışa kapalı.
- Bildirim başlığı `<p …>heading</p>` biçiminde kalır: üç yardımcı onu bu biçimle arar —
  `headingsOn` (`holdings_test.go:68`, `">"+h+"</p>"`), `apiHeadingsOn` (`tenantapi_test.go:49`, aynı)
  ve `warnNotice` (`op10_test.go:492`, `<section class="border-l-4 border-saffron[^"]*">…</section>`).
  `warnNotice`'in kesit regex'i yeni bildirimin işaretlemesine zaten değişmek zorunda; başlık `<p>`
  olmaktan çıkarsa (ör. `<h2>`) **üçü de aynı commit'te** güncellenir ve her biri, sayfada **var
  olan** bir başlığı bulduğunu gösteren bir KONTROL kazanır (boş eşleşmeyle yeşil kalamaz). "The
  public page is behind" bildirimini bulan iddia korunur.
- Her problem sabitinin başlığı, mesajı ve geri bağlantısı aynı: `TestProblemPages_LinkOnlyToMountedRoutes`,
  `TestProblemViews_TheListedBuildFormsOccurOnlyInRenderGo`, `TestOperatorHeaders_FortyResponseClassesCarryThePolicy` yeşil.
- Bildirim metni arayan bütün testler yeşil (`TestEnroll_TheWeakPasswordNoticeNamesBothLimits`,
  `TestOperatorAPI_ARefusedOrFailedOpenNoticesNothing`, `TestHoldings_TheResultIsReadNotPosted`,
  `TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`,
  `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike`).
- Uyarı zemini AA: `ink/85` saffron-lite üzerinde (`TestLegalScreen_TheTextClearsAA`) ve yeni `ok`
  zemini (`tappa-green/10` paper üstü) üzerinde ink — hesap ilgili AA testine eklenir.
- **OP-15 eki — askının altı problem sayfası** (`render.go:452-502`; screens.md §4): başlık, mesaj ve
  "Back to the tenants" kelimesi kelimesine; hepsi imzalı (bant `op-bar` ve nav var, geçerli öğe yok; tenant bandı / alt
  gezinme yok). **Dört** ret sayfası (413 "That request was too large", formu başına bir tane olmak üzere
  iki 400 "The box was not ticked", 400 "Choose one of the reasons") "Nothing was changed." der — kod
  yorumu (`render.go:453`) "three refusals" der, çünkü üç denetimi (boyut, kutu, gerekçe) sayar; belge
  sayfaları sayar; iki 503 ("The suspension was not confirmed", "Lifting the suspension was not confirmed") **demez**
  ve operatörü genel bakışa yollar; hiçbiri tenant'ı adlandırmaz, gönderilen değeri yankılamaz.
  `TestOperatorHeaders_TheSuspensionClassesCarryThePolicy` (C218–C242), `TestProblemPages_LinkOnlyToMountedRoutes`
  ve `TestSuspension_EveryRefusalWritesNothing` yeşil.
- **OP-15 eki — security-hold uyarısı** `opNotice(warn, …)`'e geçer; başlığı ("A security hold freezes
  writes. It does not contain an account."), cümleleri (`holdSentences`) ve yeri (`<div id="suspend-hold-warning"
  class="op-hold-warning">` içi, `op-reasons` ebeveyninde dört radyonun ardından) aynı. `TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice`
  uyarıyı ilk `</section>`'da keser: `opNotice` burada `<section>` kökünü korur ya da test aynı commit'te,
  kesitin başlığı hâlâ içerdiğini gösteren bir KONTROL'le güncellenir. `input.css:1711-1713`'teki üç kural
  bayt-aynı (test her birini tam bir kez arar).

---

## O2 — Ekranlar (her kart masaüstü + tablet + telefon)

Ortak ek kabul (her O2 kartı): ekran README §7'deki panolara üç genişlikte uyar; `screens.md`'deki
bölümün **her** metni kelimesi kelimesine ve her formu gizli alanlarıyla durur; "Not shown" listesindeki
hiçbir şey eklenmez; tek `<h1>`; satır içi `style=` yok; yeni JS yok; kabuk testi ve
`TestOperatorScreens_EveryActionAndLinkIsAMountedRoute` yeşil; ekranın kendi başlık testi (`…ClassesCarryThePolicy` ailesi)
yeşil; 390 / 834 / 1200 / 1440'ta yatay kaydırma yok.

## M13-04 — Giriş + kod (OpSignIn)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(operator): port sign-in and the code step to the redesign`
- **Kırmızı çizgi:** — (numaralandırma güvenliği: reddedilen girişler bayt-aynı)
- **Spec:** [screens.md §1](../design/operator-redesign/screens.md) · **Panolar:** `OpSignIn`, `T-OpSignIn`, `M-OpSignIn`
- **Dosyalar:** `web/templates/operatorpages/signin.templ`, `internal/handler/operator/signin.go` (yalnız gerekirse), `op8_test.go`, `op8_db_test.go`

İmzasız kabuk, ortalanmış kart; altı durum (varsayılan, Expired, Failed, kod, Rejected, Locked).

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `TestSurface_EveryRefusedSignInPaysOneComparisonAndAnswersAlike`,
`TestE2E_EveryRefusedSignInIsOneRowAndTheSameBytes`, `TestSignIn_ReadsTheBodyNeverTheQuery` yeşil;
yazılan adres yankılanmaz; "forgot password", kayıt, "remember me" yok.

## M13-05 — Hesap kurulumu (OpEnroll)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the enrollment page to the redesign`
- **Kırmızı çizgi:** §7 (TOTP sırrı ve kayıt jetonu yalnız tasarlanmış ilk yüklemede)
- **Spec:** [screens.md §2](../design/operator-redesign/screens.md) · **Panolar:** `OpEnroll`, `T-OpEnroll`, `M-OpEnroll`
- **Dosyalar:** `web/templates/operatorpages/enroll.templ`, `web/static/css/input.css` (`.op-qr`, `.op-key`, `.op-uri`, `.op-token-field`), `enrollqr_test.go`, `op8r2_test.go`, `op8r4_test.go`

İki adımlı kart, QR → anahtar → URI sırası, betikli/betiksiz jeton alanı, dört bildirim. Cilalı
panolar: "Step 1 · …" / "Step 2 · …" etiketleri `docket-label` değil, **17 px (telefonda 16 px) kalın
display başlık** — aynı sözcükler, yeni `.op-step` sınıfı (README §6; öğe `<h2>` olabilir, sayfada
yine tek `<h1>`). Hiçbir test bu iki satırda `docket-label`'ı sabitlemez (`a8a06f6`'da yeniden okundu, `27516b7`'de değişmedi;
`internal/handler/operator/*_test.go` içinde "Step 1"/"Step 2" geçmez), güncellenecek test yok.
Panoların QR'ı artık deponun kodlayıcısıyla basılmış **gerçek bir örnek** (sürüm 8, 57 modül × 4 px
= 228 px, mürekkep/kâğıt, `op-qr`, `role="img"`, aria-label + `<title>`); port yine `qr.go`'nun
SVG'sini (`enroll.templ:40-44`) çizer, örneğin içeriği örnek veridir.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `enroll.js`'e dokunulmaz (`TestEnrollScript_IsTheReviewedBody`, `TestEnrollScript_TouchesTheFragmentAndNothingElse`);
`enroll-token` / `enroll-token-wrap` kimlikleri aynı; `TestEnrollQR_IsTheKeysURIInkOnPaper`,
`TestEnrollQR_OnlyOnTheFirstLoad` (sayfada tek `<svg>`), `TestEnroll_TheTokenNeverTravelsInTheURL`,
`TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor` yeşil; nav yok.

## M13-06 — Konsol (OpHome)

- **Bağımlılık:** M13-02 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(operator): port the console page to the redesign`
- **Kırmızı çizgi:** — (Home okumaz)
- **Spec:** [screens.md §3](../design/operator-redesign/screens.md) · **Panolar:** `OpHome`, `T-OpHome`, `M-OpHome`
- **Dosyalar:** `web/templates/operatorpages/home.templ`, `op8_test.go`

**Tek `op-card`** (kodun bugünkü biçimi, `home.templ:22-47`; docket değil): mono etiket "Operator
console", her genişlikte 28 px `<h1>`, dört bağlantı + açıklaması tablet/masaüstünde **iki sütunlu
ızgara**, telefonda **alt alta 44 px bağlantılar**, oturum cümlesi kartın **içinde** alt bilgi (README
§6.1 "Home hub"). **İkon kutucukları taşınmaz** (D1):
panoların eski bir sürümü her bağlantının yanına 48 px ikon kutucuğu çiziyordu; kutucuklar
`OpHome` / `T-OpHome` / `M-OpHome`'dan kaldırıldı — Home'da bugün yok ve imzalı sayfanın SVG'leri
tam olarak nav'ın beşi.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `<a href="/operator/holdings"
class="op-link">Holdings</a>` ve `…/audit" class="op-link">Audit log</a>` aynen; sayı, widget, operatör
adı/e-postası yok; Home'un store çağrısı 0.

## M13-07 — Tenant listesi ve arama (OpTenants)

- **Bağımlılık:** M13-02 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the tenant list to the redesign`
- **Kırmızı çizgi:** §7 (arama terimi kişisel veri: URL'ye, bağlantıya, problem sayfasına girmez)
- **Spec:** [screens.md §5](../design/operator-redesign/screens.md) · **Panolar:** `OpTenants`, `T-OpTenants`, `M-OpTenants`
- **Dosyalar:** `web/templates/operatorpages/tenants.templ` (`Tenants`, `tenantList`, `tenantPager`, `tenantPageForm`), `web/static/css/input.css` (satır ızgarası, kapsamlı seçiciler), `op11_test.go`, `internal/handler/operator/op15_test.go` (yalnız sabit liste işaretlemesi bilerek değişirse)

Arama kartı (`role="search"`, POST), sonuç satırı + "Show every tenant", docket satırları, üç boş durum,
POST sayfalayıcı, askıdaki tenant'ın **Suspended** çipi (OP-15). Cilalı panolar (ölçüldü): satır
masaüstünde tek satır, dört iz (ad · Since · plan · id — `OpTenants`), tablette üç iz + id altta tam
genişlik (`T-OpTenants`), telefonda ad bağlantısı ilk satırda tek başına, 44 px (README §6.1 "Tenants
row"; kapsamlı seçici — `<li class="op-row">` aynen; README'nin önceki tarifi tablete de dört iz
veriyordu, düzeltildi). Sayfalayıcı cilalı listede değil: `op-link` kalır.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `TestTenantSearch_NoLogLineHeaderOrOtherPageCarriesTheTerm`,
`TestTenantList_PagesForwardOnlyAfterAFullPage`, `TestTenantScreens_EscapeWhatATenantAndAnOperatorTyped`,
`TestTenantScreens_TheTextClearsAA` yeşil; `op11_test.go:845` ve `:997` işaretlemesi aynen; sayaç,
süzgeç, sıralama, "son aramalar" yok.
- **OP-15 eki — Suspended çipi ve ızgara:** `TestTenantList_ASuspendedTenantCarriesTheChip` yeşil —
  `<span>plan</span>`, `<span class="tally tally--tenant-suspended">Suspended</span>`,
  `<span class="break-all text-xs text-ink/70">id</span>` bu sırayla, ve bir askıdaki + bir aktif
  tenant'lı listede **listenin tamamında** tam bir çip (aktif satırda çip yok); bu yüzden planın mono'su ve id'nin 12 px'i kapsamlı seçiciden gelir (`> :nth-child(3)`,
  `> :last-child`), bu span'lere sınıf eklenmez. Izgara: bugün satır 4 çocuk, askıda 5 (çip plan ile id
  arasında, `tenants.templ:81-83`); id (son çocuk) açıkça yerleşir — masaüstünde 1. satır 4. iz,
  tablette tam genişlik — ve çip otomatik yerleşimle **adın altındaki 2. satıra** düşer; çipsiz satır
  panonun tek satırı kalır; `md:justify-items-start` çipi kendi genişliğinde tutar (README §6.1).
  Hiçbir yeni seçici `.tally` adını taşımaz (`TestTallyRules_NoToneRuleColoursTheWord` sayar). Fixture
  "tenants, a suspended row" (ve bir çipsiz satır yanında) 834 / 1200 / 1440 / 390'da ekran görüntüsü:
  çipli ve çipsiz satırda Since / plan / id hizası aynı, yatay kaydırma yok.
  `TestSuspensionScreen_TheChipsAndTextClearAA` yeşil; listede gerekçe, başlangıç zamanı, "yalnız
  askıdakiler" süzgeci ya da satırda askı denetimi yok.

## M13-08 — Tenant özeti + entegrasyon API'si (OpTenant)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the tenant overview to the redesign`
- **Kırmızı çizgi:** §4.5 istisna (1) (OP-11 en aza indirme: kişi, adres, uid, VAT, holding, anahtar, webhook yok; askının iki `op_*` yazması), ADR 0020 §9, ADR 0025 §1 ve §5 (kapalı gerekçe kümesi, tenant yoldan, PRG)
- **Spec:** [screens.md §6](../design/operator-redesign/screens.md) (+ "Panel writes card") · **Panolar:** `OpTenant`, `T-OpTenant`, `M-OpTenant` (askı kartı panolarda yok)
- **Dosyalar:** `web/templates/operatorpages/tenants.templ` (`TenantOverview`, `tenantFact`, `tenantCount`, `tenantAPISection`, `apiNotice`, `tenantSuspension`, `suspensionReason`, `suspendForm`, `holdWarning`, `reinstateForm`), `op11_test.go`, `tenantapi_test.go`, `op15_test.go` (yalnız sabit işaretleme bilerek değişirse)

Olgular kartı (ilk satırı sayfanın `<h1>`'i olur — aşağıda), "Live now" docket'i, üç bağlantı, API bölümü (durum satırı, cümle, form, Close), altı
bildirim + kurallar, ve OP-15'in **Panel writes** kartı (durum çipi, askıda iki olgu, duruma göre tek
form: dört gerekçe radyosu + security-hold uyarısı + kutu + "Suspend writes", ya da kutu + "Lift the
suspension"). Askı kartının panosu yok: screens.md §6 "Panel writes card"daki tarifle, bileşen
kataloğunun parçalarından kurulur (`op-card`, `docket-label`, `.op-main .tally`, `dl`, radyo ızgarası,
`opNotice(warn)`, `btn`).

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `dt`/`dd` regex'leri (`op11_test.go:894`, `:900`) aynen; açık API'de tam
bir açma ve bir kapama formu, kapalıda yalnız açma formu; `TestTenantOverview_NamesTheTenantInTheBannerAndRefusesABadPath`,
`TestTenantOverview_AnUnnamedTenantIsNamedByItsID`, `TestOperatorAPI_TheResultIsReadNotPosted`,
`TestOperatorAPI_TheCodeRuleIsTheDatabases` yeşil; onay adımı eklenmez.
- **Sayfa başlığı — orkestratör kararı (2026-10-25):** genel bakış bugün `<h1>`'siz tek operatör veri
  ekranıdır (ölçüldü: `tenants.templ:129` yalnız `<p class="docket-label">Tenant overview</p>`; Plaques,
  Billing, VAT'ta tek `<h1>` sayfa başlığıdır — `plaques.templ:20`, `billing.templ:28`, `vat.templ:25`).
  `<h1>` **sayfa başlığıdır, tenant adı değil**: o öğe `<h1 class="docket-label">Tenant overview</h1>`
  olur — sözcük ve sınıf aynı, yalnız öğe (onu sabitleyen test yok); görünüşü `docket-label` kalır, katalogdaki display boy `<h1>` tarifi ona uygulanmaz (README §6 "Card" istisnası; pano da küçük etiket çizer). Tenant adı bandı `<h1>` olmaz
  (kabuğundur; öteki üç ekranda iki `<h1>` çıkar ve bandın sabit `<p class="font-display text-xl
  font-bold tracking-tight">`'ı bozulur). **Yeni test** (adını yapıcı koyar): dört tek-tenant ekranın
  `screens()`'teki her render'ı (overview / plaques / billing / VAT fixture'ları) tam **bir** `<h1`
  taşır, o `<h1` `<main`'in içindedir ve tenant bandının (`<section class="op-tenant"`) içinde değildir;
  genel bakışınki `Tenant overview`'dur; KONTROL: `<h1>`'siz bir genel bakış render'ı (bugünkü
  işaretleme) kırmızıdır.
- **OP-15 eki — Panel writes kartı** (screens.md §6): API kartından sonra, alt bağlantılardan önce; her
  metin, iki form ve üç durum (aktif, askıda, tanınmayan gerekçe) kelimesi kelimesine; sabit işaretleme
  bayt-aynı (radyonun öznitelik sırası, `op-reason-hold` sınıfının başta olması ve
  `aria-describedby`, etiket, kutu, `<div class="op-reasons mt-1">` içinde dört radyonun ardından
  `<div id="suspend-hold-warning" class="op-hold-warning">`, iki çip, `Suspended since`/`Reason`
  `dt`/`dd`'leri); `dl`'in `sm:grid-cols-2`'si `md:grid-cols-2` olur. `TestTenantOverview_SaysTheSuspensionAndOffersOneForm`,
  `TestSuspendForm_TheHoldWarningIsShownOnlyByTheHoldChoice`, `TestSuspensionWords_NameEveryReasonAndNothingElse`,
  `TestSuspensionScreen_TheChipsAndTextClearAA`, `TestSuspensionScreen_EachChipHasTheRuleTheContrastTestComputes`,
  `TestSuspension_TheListedSitesAloneWrite` yeşil; `TestOperatorScreens_EveryActionAndLinkIsAMountedRoute`'un
  askı KONTROL'ü (`op8_test.go:921-926`: aktifte yalnız suspend, askıdakinde yalnız reinstate formu)
  yeşil; tam koşuda `TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant` ve
  `TestE2E_TheOperatorsSuspensionClosesThePanelAndTheAPIAndItsLiftingOpensThem` yeşil. Üç fixture'ın
  ekran görüntüsü üç genişlikte; Security hold seçiliyken uyarının göründüğü ve başka seçimde gizlendiği
  bir kez tarayıcıda gözle doğrulanır (testler yalnız işaretleme + kuralları ölçer).

**Güvenlik merceği — askı formları** (agent `tappa-security-auditor`, diff üstünde; işi yapan bakmaz):
1. **Yalnız POST, tek form:** iki `action` aynı kökenli bağlı yol (`/operator/tenants/{id}/suspend`,
   `…/reinstate`, `routes.go:111-112`), açılış etiketinde `method` `action`'dan önce; genel bakışta
   duruma göre tam **bir** askı formu. Form GET'e, bağlantıya ya da düğmesiz bir JS eylemine dönmez;
   `formaction` / `formmethod` eklenmez.
2. **CSRF ve kimlik:** savunma konsol grubunun zinciridir — `floodGate → sameOriginGate(true) →
   requireOperator → sessionGate` (`routes.go:96-119`) — ve oturum çerezinin SameSite=Strict'i
   (`suspension.go:37-39`). Port CSRF jetonu alanı eklemez, onay kutusunu CSRF savunması saymaz, yeni
   rota/handler eklemez; `SuspensionStore` çağrı yerleri aynı (`TestSuspension_TheListedSitesAloneWrite`,
   SU1–SU5).
3. **Tenant yalnız yoldan:** formda tenant id'si taşıyan gizli ya da görünür alan yok — suspend formunun
   alanları tam `reason` + `confirm`, reinstate'inki tam `confirm`
   (`TestTenantOverview_SaysTheSuspensionAndOffersOneForm`, `TestSuspension_TheTenantIsThePathsNotTheForms`).
4. **ADR 0025 §1 — gerekçe zorunlu ve kapalı küme:** dört radyo `name="reason"`, `required`, değerleri
   `db.SuspensionReasons()` sırasıyla kodlar; serbest metin, `<textarea>`, `type="text"`, "Other"
   seçeneği yok; varsayılan seçili radyo yok (yanlış gerekçe sessizce gitmesin). İstemcinin `required`'ı
   tek savunma değildir: sunucu kutuyu (tam `yes`) ve gerekçeyi (kapalı küme, kırpmasız) store'dan önce
   yine denetler — 400 (`TestSuspension_NothingIsWrittenWithoutTheBoxOrAKnownReason`). Gerekçesiz askı
   yolu açılmaz.
5. **Onay:** kutu `name="confirm" value="yes" required`; modal / JS onayı yok (genel bakışın CSP'sinde
   `script-src` yok); onay adımı (ikinci sayfa) eklenmez.
6. **Yankı ve sızıntı:** problem sayfaları gönderilen hiçbir değeri ve tenant adını taşımaz; ret
   günlük satırı yazmaz. Başarılı yazmanın günlük satırı (`suspension.go:187-188`) tenant id'si,
   operatör id'si ve gerekçe **kodunu** yazar — `Known()`'dan geçmiş, kapalı kümenin bir üyesi; serbest
   metin ya da ham gönderilen değer değil (`TestSuspension_NoPostedValueReachesALogLine`'ın PART I'i
   buna izin verir), `TestOperatorHeaders_TheSuspensionClassesCarryThePolicy`,
   `TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`. Port bu satıra alan eklemez. Security-hold
   uyarısı kapalı metindir, tenant verisi taşımaz.
7. **Okuma ve `op_*`:** yeni `op_*`, yeni okuma, genişlemiş dönüş sütunu yok; kartın durumu genel
   bakışın kendi okumasından; yazma bir istek birimi, okuma birimi değil
   (`TestSuspensionBudget_AWriteIsOneRequestAndNoRead`).
8. **PRG ve dürüstlük:** başarı 303 → genel bakış; başarı bildirimi ya da "zaten askıda" / "hiçbir şey
   değişmedi" cümlesi eklenmez (ADR 0021 §2 v 7); 503 sayfaları "kaydedilmedi" demez.
9. **Uyarının görünürlüğü** yalnız CSS genel kardeş kuralıyla kalır (stil dosyası yoksa görünür —
   uyarı için güvenli yön); `:has()`'e ya da betiğe çevrilmez.

## M13-09 — Plaket envanteri (OpPlaques)

- **Bağımlılık:** M13-02 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(operator): port the plaque inventory to the redesign`
- **Kırmızı çizgi:** §4.7 (anahtar, anahtar ref, CMAC, tap URL'si sayfaya gelmez)
- **Spec:** [screens.md §7](../design/operator-redesign/screens.md) · **Panolar:** `OpPlaques`, `T-OpPlaques`, `M-OpPlaques`
- **Dosyalar:** `web/templates/operatorpages/plaques.templ`, `op13_test.go`

Özet kartı (üç cümleden biri), docket satırları, yedi çip, adsız tenant bandı (cilalı `OpPlaques`
panolarında da bir durum). Cilalı panolar: satır tablet/masaüstünde **iki sütun** — UID kendi
sütununda, çip + cümle + olgular yanında tek bir `<div>` içinde; telefonda alt alta (README §6.1
"Plaque row"). Satır yine UID span'iyle **başlar** (`op13_test.go:285`); `<div>` form ya da düğme
değildir.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: sayfada tam bir `<form` (sign-out), tam bir `<button`
(sign-out'un metin düğmesi), hiç `hx-` (`op13_test.go:327-332`);
`TestPlaqueScreen_NoKeyReachesThePage`, `TestPlaqueWords_NameEveryShapeAndNothingElse`,
`TestPlaqueScreen_TheFirst200OfNAndAnEmptyTenant`, `TestPlaqueScreen_TheChipsAndTextClearAA` yeşil;
plaket QR'ı ya da çizimi yok.

## M13-10 — Fatura ayları (OpBilling)

- **Bağımlılık:** M13-02 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(operator): port the billing months to the redesign`
- **Kırmızı çizgi:** §4.6 (okunamayan / kayıt-öncesi ayda rakam yok, "not even a zero")
- **Spec:** [screens.md §8](../design/operator-redesign/screens.md) · **Panolar:** `OpBilling`, `T-OpBilling`, `M-OpBilling`
- **Dosyalar:** `web/templates/operatorpages/billing.templ`, `op12_test.go`, `op12_db_test.go` (`screenFact`)

Giriş kartı, docket (beş durum, serbest pencere, taban cümlesi, son sayfa notu), POST sayfalayıcı.
Cilalı panolar:
- **Fiş şeridi:** dört rakam (Plan · People counted · Price per person · Amount) satır içi
  `<span class="text-sm">Plan <span class="font-mono">…</span></span>` yerine küçük etiket üstte, değer
  altta bir `<dl class="op-receipt">` — etiketler PLAN · PEOPLE COUNTED · PRICE PER PERSON · AMOUNT
  (kodun sözcükleri, büyük harf CSS'ten), Amount sağa yaslı 20 px kalın; telefonda 2 × 2 (README §6
  "Billing receipt strip"). Closed / First charged month / Counted over satır içi span kalır.
- **Sayfalayıcı** "Newer months" / "Older months" çerçeveli düğme: `<button type="submit"
  class="op-pager">` (README §6 "Pager").
- **Çip tonları:** canlı = kesikli safran çerçeve (`tally--running`'in yeni tonu, M13-02), bitmiş ama
  kapanmamış = safran, dondurulmuş = yeşil, kayıt öncesi = çizgi, okunamayan = mürekkep.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve:
- **Fiş şeridinin test güncellemesi, aynı commit'te, gevşetmeden:** dört rakamı okuyan her yer yeni
  işaretlemeyi okur — `fact()` yardımcısı (`op12_test.go:192`; bu dört etiket için yeni biçimi döndürür,
  ötekiler için eskisini), `TestBillingScreen_EveryStateSaysItsWordAndNoVerdict`'in döngüsü
  (`op12_test.go:513-517`), `TestBillingScreen_WearsTheDocketAnatomy`'nin döngüsü (`op12_test.go:1436-1440`;
  dört rakam artık şeridin `dt`/`dd` çiftinde mono değerle, üçü eski span'de) ve uçtan uca okuyucu
  `screenFact` (`op12_db_test.go:167`; şerit hücresini de okur, `len(r.facts)` karşılaştırması
  `op12_db_test.go:343` aynı kalır). KONTROL: rakamlı bir satırda tam dört şerit hücresi bulunur ve
  eski `<span class="text-sm">Plan <span class="font-mono">` biçimi sayfada **yoktur**; kayıt öncesi
  ve okunamayan satırda şerit hiç yoktur (§4.6, "not even a zero").
- **Sayfalayıcının test güncellemesi, aynı commit'te:** `TestBillingScreen_PagesAreFiveOfTwelve`'in
  `form` kalıbı (`op12_test.go:1370-1373`) ve `TestBillingScreen_WearsTheDocketAnatomy`'nin düğme
  iddiası (`op12_test.go:1441`) `class="op-pager"` okur; öğe (`<button`), `type="submit"`, formun
  `method="post"` + `action` yolu, gizli `page` alanı ve etiket iddiaları aynen kalır; KONTROL:
  `class="op-link">Older months</button>` / `Newer months</button>` sayfada yok.
- **AA satırının etiketi, sayfalayıcıyla aynı commit'te:** `TestBillingScreen_TheChipsAndTextClearAA`'nın
  `"tappa-green on porcelain (the pager, outside the docket)"` satırı (`op12_test.go:1510`) ve doc yorumu
  (`op12_test.go:1467-1468`) artık sayfalayıcıyı ölçmez — `op-pager`'ın sözcüğü paper üstünde ink'tir.
  Satır **silinmez** (hesap ve 4.5:1 eşiği aynen); etiketi ve yorumu sayfalayıcıyı anmayacak biçimde
  yeniden yazılır, sayfalayıcı "ink on paper" satırının etiketine eklenir; `op-pager`'ın `ink/30`
  çerçevesi metin değildir (README §6 "Pager"). Hiçbir satır, ölçmediği bir öğeyi ölçtüğünü söylemez.
- `TestBillingScreen_WearsTheDocketAnatomy`, `TestBillingScreen_EveryStateSaysItsWordAndNoVerdict`,
`TestBilling_AFrozenMonthAndALiveMonthDoNotRenderAlike`, `TestBillingScreen_OffersNoItemisationAnywhere`,
`TestBillingScreen_ReadsNothingFromTheURL` yeşil; page 3'te tam iki sayfalayıcı formu; ödendi/gecikti
sözcüğü, fatura PDF'i, kişi listesi yok.

## M13-11 — VAT yeniden denetimi (OpVat)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the VAT re-check to the redesign`
- **Kırmızı çizgi:** §7 (VAT numarası yalnız bu ekranda), §4.5 istisna (1) (definer `vat_number`'ı güncellemez)
- **Spec:** [screens.md §9](../design/operator-redesign/screens.md) · **Panolar:** `OpVat`, `T-OpVat`, `M-OpVat`
- **Dosyalar:** `web/templates/operatorpages/vat.templ`, `op16_test.go`

"On file" docket'i, dört karar çipi, zaman satırı, "Ask again" kartı (alansız form) ya da
sorulamaz cümlesi, üç uyarı, adsız tenant bandı (cilalı `OpVat` panolarında da bir durum; fixture
"tenant VAT, not found"). Cilalı panolar: "On file"un iki olgusu tablet/masaüstünde **yan yana**
(`dl`, `vat.templ:41` → `md:grid md:grid-cols-2`), telefonda alt alta; sabit
`<dd class="mt-1 break-all font-mono text-lg font-bold">` (`op16_test.go:1409`) aynen.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `TestVATScreen_TheFourStatesAreCalledWhatTheAccountScreenCallsThem`,
`TestVATScreen_EveryStateSaysItsWordAndSentence`, `TestVATRecheck_TheNumberIsTheServersNotTheClients`,
`TestVATRecheck_TheNumberIsOnNoLogLineOrHeader` yeşil; sorulabilirde tam bir form, garip numarada
hiç; numara düzenleme alanı yok; başarı bildirimi yok.

## M13-12 — Denetim günlüğü (OpAudit)

- **Bağımlılık:** M13-02 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the audit log to the redesign`
- **Kırmızı çizgi:** §7 (arama sınıfı, asla terim; kimlik bilgisi alanı yok)
- **Spec:** [screens.md §10](../design/operator-redesign/screens.md) · **Panolar:** `OpAudit`, `T-OpAudit`, `M-OpAudit`
- **Dosyalar:** `web/templates/operatorpages/audit.templ`, `op14_test.go`, `op14d_test.go`

Süzgeç kartı (`<select id="audit-kind" name="kind" class="op-input">`; OP-15'ten beri Every kind + 22
tür), docket satırı (her olgu bir span), boş/son sayfa durumları, POST sayfalayıcı; OP-15'in iki türü
"Tenant suspension recorded" / "Tenant reinstatement recorded" (screens.md §10). Cilalı panolar: operatör ve hesap **adları Space
Grotesk** (kod onları mono'suz `<bdi>` içinde yazar, `audit.templ:92, 98`); mono yalnız kodun bastığı
yerde — zaman, sayfa, oturum, kimlikler, belge, bayt. "Of a scope this screen does not show" satırı
kodda var (`audit.templ:117-118`) ve listelenir. Satır içi tenant bağlantısı tablet/masaüstünde satır
yüksekliğini korur, **44 px görünmez örtü** alır (README §6 "Inline link hit area"; işaretleme
`op14_test.go:317, 328, 387`'de sabit, değişmez). Sayfalayıcı çerçeveli düğme: `class="op-pager"`.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: `TestAuditScreen_WearsTheDocketAnatomy`
(başlık işaretlemesi aynen; sayfalayıcı iddiaları `op14_test.go:742-743` **aynı commit'te**
`<button type="submit" class="op-pager">Previous page</button>` / `…Next page</button>` olur — öğe,
`type`, etiket aynen; `TestAuditScreen_ReadsNothingFromTheURL`'ün form `method`/`action` ve gizli
`kind` + `page` iddiaları değişmez; KONTROL: `class="op-link">Previous page</button>` sayfada yok),
**AA satırının etiketi aynı commit'te** — `TestAuditScreen_TheTextClearsAA`'nın doc yorumu
(`op14_test.go:1014-1015`, "and on porcelain (the pager's links)") ve `"tappa-green on porcelain"` satırı
(`op14_test.go:1055`) sayfalayıcıyı anmayacak biçimde yeniden yazılır, satır ve eşiği silinmeden;
sayfalayıcının sözcüğü "ink on paper" satırında (`op14_test.go:1051`) adıyla anılır. (Tenant listesinin
eşi `op11_test.go:1064`/`:1109` doğru kalır: `tenantPageForm` `op-link` kalıyor ve özetin alt
bağlantıları sayfa zeminindedir — M13-07 sayfalayıcıyı değiştirirse aynı düzeltme orada yapılır.) `TestAuditWords_NameEveryKindAndNothingElse`,
`TestAuditScreen_EveryRowSaysWhatTheLogHolds`, `TestAuditScreen_NoCredentialFieldReachesThePage`,
`TestAuditScreen_ReadsNothingFromTheURL` yeşil; süzülmüş sayfada tam üç form; dışa aktarım, tenant
başına görünüm, türetilmiş hüküm yok.
- **OP-15 eki — iki audit türü:** "Tenant suspension recorded" / "Tenant reinstatement recorded"
  (`audit.go:226-227`) süzgeçte iki seçenek (kapalı küme sırası, `internal/db/operator.go:893-903`) ve
  satırda tenant'lı satır biçiminde: zaman · tür · aktör · Tenant olgusu (bağlantı / adsız / "A tenant
  id no tenant has", `audit.templ:103-114`); satırın `detail`'i `{}` — **gerekçe bu ekranda yok**
  (tenant'ın kendi `audit_log`'undadır). `TestAuditWords_NameEveryKindAndNothingElse` (22 tür) yeşil;
  tam koşuda `TestE2E_OperatorSuspendsAndReinstatesAFixtureTenant`'ın günlük adımı
  (`op15_db_test.go:205-208`: tür sözcüğü + genel bakış bağlantısı + tenant adı) yeşil. Türetilmiş
  "suspended" hükmü ya da çipi eklenmez: sözcük eylemin **kaydedildiğini** söyler, durumun değiştiğini
  değil.

## M13-13 — Holding'ler (OpHoldings)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(operator): port the holdings screen to the redesign`
- **Kırmızı çizgi:** §4.5 istisna (1) (`op_*`) — istisna (2)'nin holding üyeliğini yalnız bu yüzeyin `op_*`'ı yazar (ADR 0027 §2); yeniden adlandırma/silme yok
- **Spec:** [screens.md §11](../design/operator-redesign/screens.md) · **Panolar:** `OpHoldings`, `T-OpHoldings`, `M-OpHoldings`
- **Dosyalar:** `web/templates/operatorpages/holdings.templ`, `holdings_test.go`

Oluşturma kartı, dokuz bildirim + kurallar, holding girdileri (üye satırları, Live/Ended, Remove, ekleme
formu, sayfa bölünmesi cümleleri), **bağlantı** sayfalayıcısı. Cilalı panolar:
- "Create a holding" **19 px başlık** (`.op-heading`; bugün `<h2 id="holding-create" class="docket-label">`,
  `holdings.templ:40`) — `id` kalır (kartın `aria-labelledby`'si); bu sınıfı sabitleyen test yok.
- Üye satırında tenant id **adın altında** — yalnız görsel sıra (CSS `order`, README §6.1
  "Holdings"); DOM sırası kodunki (`holdings.templ:131-160`), hiçbir test onu sabitlemez.
- Sayfalayıcı çerçeveli düğme görünümünde **bağlantı** kalır: `<a href="/operator/holdings?page=N"
  class="op-pager">`. Bağlantının sınıfını sabitleyen test yok; `href` iddiaları
  (`op8_test.go:917-918`, `holdings_test.go:741, 757`) aynen.
- "page 3, past the end" durumu ("This page is empty: the list ended on the page before.",
  `holdings.templ:51-52`; fixture "holdings, past the end") panolarda da var.

**Kart içi boşluk (M13-02'den devreden):** bu ekranın `op-card`'larında kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdelerin `mt-*`'leri aynı commit'te kalkar (README §6 "Card").

**Kabul kriterleri.** O2 ortak kabulü ve: form sayıları
(`op8_test.go:911-913`) ve çip işaretlemesi (`holdings_test.go:~689, ~987`) aynen;
`TestHoldings_CreateAddRemoveThroughTheScreen`, `TestHoldings_TheResultIsReadNotPosted`,
`TestHoldings_PagesForwardOnlyAfterAFullPage`, `TestHoldingsScreen_EscapesWhatOperatorsAndBusinessesNamed`
yeşil; reddedilen ad yankılanmaz; tenant seçici, holding sayfası, onay adımı yok.

## M13-14 — Yasal metinler (OpLegal)

- **Bağımlılık:** M13-03 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(operator): port the legal texts editor to the redesign`
- **Kırmızı çizgi:** —
- **Spec:** [screens.md §12](../design/operator-redesign/screens.md) · **Panolar:** `OpLegal`, `T-OpLegal`, `M-OpLegal`
- **Dosyalar:** `web/templates/operatorpages/legal.templ`, `op10_test.go`

Giriş kartı (müşteri sitesi `/legal` metin, bağlantı değil), dört düzenleyici kartı (canlı/yayımlanmamış,
"behind" uyarısı, textarea, düğme), sürüm docket'i (`<li class="op-version">`, Live çipi, "tenant admin
(legacy)"), boş ve 100 sınırı notları. Cilalı panolar (README §6.1 "Legal editor", "Legal versions"):
- Belge başına **tam genişlik tek düzenleyici kartı**, yan iç boşluğu tek kart ölçeği: masaüstü / tablet /
  telefon **24 / 24 / 20 px** (panoların `--edpx`'i 32 / 26 / 18'den hizalandı); başlık satırında solda yol (mono), sağda dekoratif
  durum noktası (`aria-hidden`; durumu sözcükler taşır) + "Live since …" / "Not published…"; "The public
  page is behind" uyarısı başlık ile form **arasında** (kodun sırası); "TEXT" etiket çubuklu
  **çerçeveli textarea**, mono 14 px / 1.7, `rows="12"`; solda yardım, sağda **Publish a new version**
  (telefonda tam genişlik düğme).
- **Yardım metni (legal-links, ADR 0033 — paket yazıldıktan sonra):** textarea'nın altındaki yardım artık
  bağlantı kuralını da söyler (`legal.templ:70-74`: "… Addresses starting with http:// or https://,
  e-mail addresses and phone numbers starting with + become links; `[text](https://…)` links your own
  text. At most 256 KiB."); panolar eski iki cümleyi çizer — kod kazanır (D1), metin aynen.
- Sürüm listesi tablet/masaüstünde **beş sütunlu ızgara** (zaman kalın mono · yol · bayt sağa yaslı ·
  yayımlayan · Live çipi), telefonda alt alta kartlar; kapsamlı seçici — `<li class="op-version">`
  aynen (`op10_test.go:308, 631, 656`).

**Kart içi boşluk (M13-02'den devreden):** `gap-3.5` yalnız **giriş kartında** (`legal.templ:23`): kart içi boşluk `gap-3.5`'e geçer (`flex flex-col gap-3.5`, işaretlemede), gövdesinin `mt-*`'leri aynı commit'te kalkar (README §6 "Card"). **Düzenleyici kartı hariç** (belge başına `op-card`, `legal.templ:47-77`; README §6.1 "Legal editor"; panoda `.ed{gap:0;padding:0;overflow:hidden}`): kartın kendisi **dolgusuz ve `gap-0`**, bantlara bölünür; ölçeğin 24 / 24 / 20 px'i bantların yan boşluğudur.

**Kabul kriterleri.** O2 ortak kabulü ve: `TestLegalPage_ShowsWhoPublishedEachVersion`,
`TestLegalPublish_AFailedRefreshRedirectsAndTheScreenSaysThePageIsBehind`,
`TestLegalScreen_ReopensOnPastedMarkupWithoutExecutingIt`, `TestLegalScreen_TheTextClearsAA` yeşil;
eski sürüm metni, önizleme, fark, "bu sürümü yeniden yayımla" yok.

---

## O3 — Kapanış

## M13-15 — Son tam doğrulama ve güvenlik denetimi

- **Bağımlılık:** hepsi · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Kırmızı çizgi:** §4.5 istisna (1) (bütün tenant ötesi yüzey), §4.7, §7 (operatör günlük/gösterim listesi)
- **Commit:** `docs(brand): record the operator shell and its components` (+ yalnız düzeltme commit'leri)

Tam `make check` (fmt + lint + `-race` test), `make audit` bir kez; bütün M13 diff'i üzerinde güvenlik
denetimi: yeni `op_*` ya da genişlemiş dönüş sütunu yok, store çağrı sayıları ve okuma bütçesi
değişmedi, tasarlanmış sızıntı alanları dışında sır/terim/VAT/holding adı/operatör e-postası yok
(`TestLeak_NoOperatorCredentialOnASurfaceItWasNotMeantFor`), CSP ve betik kümesi aynı, POST gövdeleri
URL'ye çıkmadı, tenant markası yok, tenant adı bandı her tek-tenant ekranında; 390 / 834 / 1200 / 1440
ekran görüntüsü turu (imzalı + imzasız + tek-tenant). Skill `tappa-brand`'e operatör kabuğunun ve `op-*`
bileşenlerinin özeti (tarihli not), tasarım README'sinin bileşen kataloğu gerçekleşen sınıf adlarıyla
güncellenir.

**Dosyalar.** `.claude/skills/tappa-brand/` (operatör notu), `docs/design/operator-redesign/README.md`
(katalog), yalnız düzeltme gerektiren kod/test dosyaları.

**Kart içi boşluğun toplanması (M13-02'den):** M13-04…M13-14 her operatör `op-card`'ına `flex flex-col gap-3.5` yazdıktan sonra üç yardımcı `.op-card` kuralına taşınır ve işaretlemeden düşer (README §6 "Card"); işaretlemede kalan `op-card flex flex-col gap-3.5` yok. **İstisna korunur:** yasal metin düzenleyici kartı (M13-14; README §6.1 "Legal editor", panoda `.ed{gap:0;padding:0}`) taşımadan sonra da **dolgusuz ve `gap-0`** kalır — `.op-card`'ın yeni boşluğu ve dolgusu ona geçmez (M13-14'ün koyduğu geçersiz kılma ya da kapsamlı kural toplamada silinmez); KONTROL olarak taşımadan sonra düzenleyicinin bantları 390 / 834 / 1440'ta panoyla karşılaştırılır.

**Kabul kriterleri.** `TestEveryNamedTestExists` yeşil (belgede adı geçen her test var). Bulgu →
düzeltme → yeni denetçi. Bittiğinde `state.md`; `main`'e birleştirme **kullanıcının deploy kararıdır**.
