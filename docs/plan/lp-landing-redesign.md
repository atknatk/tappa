# LP — Public site yeniden tasarımı: landing, yeni sayfalar, ortak kabuk, 404 ve paylaşım kartları

> **Durum:** plan (2026-10-21). Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya
> "yapıldı" yazılmaz.
>
> **Teslim paketi (okumadan başlama):** [docs/design/landing-redesign/README.md](../design/landing-redesign/README.md)
> — panoların okunuşu (§1), K1–K4 ve orkestratör kararları (§2), pano ↔ rota ↔ kod haritası (§3),
> her sayfanın miras aldığı kısıtlar (§4), `.lp` CSS stratejisi (§5), token/bileşen envanteri (§6),
> **olgu pinleme** düzeneği ve yeni sayfanın ona katılışı (§7), public 404'ün dokunduğu yerler (§8),
> paylaşım kartları (§9), **doğrulanmamış iddialar** (§10), korunacak testler (§11). `file:line`
> atıfları `origin/main` `0e7c158`'e göredir. Panolar: `docs/design/landing-redesign/boards/`
> (`<Ad>`, `T-<Ad>`, `M-<Ad>`; 23 dosya).

## Kullanıcı kararları (2026-10-21) — bağlayıcı

- **K1** — Gerçek şirket ve şube adları (Kebab Factory Ltd., Kebab Manufacturing Co., şube/departman
  listesi) For* sayfalarında public **kalır**; tablolar seed'e eşit tutulur, "from our demo data"
  etiketi kalır.
- **K2** — Barınma metni **"Hosted in the EU (Germany)"**; Falkenstein / veri merkezi / sağlayıcı adı
  yazılmaz.
- **K3** — OG görselleri tasarım tuvalinden **PNG export** edilip `web/static/og/` altına commit
  edilir; yeni bağımlılık yok, Go üretimi yok.
- **K4** — Public **404 sayfası eklenir**, yeni landing kabuğuyla (header/footer + tek cümle + ana
  sayfa linki).
- **Sıra:** önce landing/public site (bu faz), sonra operatör (M13).

**Orkestratör kararları** (README §2 tablosu O1–O8, gerekçeleriyle): Product'taki "guarantee"
cümleleri yeniden yazılır, regex istisnası yok (O1) · hero animasyonları kaldırılır (O2) · `.lp`
kapsamlı CSS ile devam (O3) · yeni sayfalar olgu pinleme rejimine ve `marketingURLs()`'e girer (O4) ·
Signup/Legal/API kılavuzu yeni ortak kabuğa geçer (O5) · rotalar `/product`, `/pricing`,
`/security`, `/for/restaurants`, `/for/production` (O6) · Pricing'in "Frozen" kaşesi panelin emsali
`stamp stamp--approved` + FROZEN (O7) · **bağlantı rotasıyla birlikte gelir** (O8 — aşağıda).

## Sıralama — boşluk raporundaki §6 sırasından ne değişti ve neden

Raporun sırası: LP-01 CSS · LP-02 ortak kabuk (nav **sayfa linkleri** + test) · LP-03 hero · LP-04
How/FAQ/**more linkleri** · LP-05 The panel · LP-06 /pricing · LP-07 /security · LP-08 /product ·
LP-09 /for/restaurants · LP-10 /for/production · LP-11 fits + more linkleri yeni rotalara · LP-12 OG ·
LP-13 Signup/Legal/API kabuğu · LP-14 404 · LP-15 faz sonu.

Doğrulanan sorun: `TestMarketing_EveryInternalLinkResolves` (`marketing_test.go:759-809`) her public
URL'deki her `href`'i router'dan ister ve 200 bekler. Raporun LP-02'si (header'da `/product`,
`/security`, `/pricing`) ve LP-04'ü ("Pricing in detail →", "How each proof works →") hedef rotalar
LP-06…LP-08'de doğmadan bağlantı ekler → o commit'lerde test **kırmızı**, kart tek başına sevk
edilemez. LP-11 ise yalnız "bağlantıyı sonradan eklemek" için vardı, yani rotasından ayrılmış bir
bağlantının telafisiydi.

Düzeltmeler:
1. **O8 — bağlantı rotasıyla aynı commit'te.** LP-02 kabuğu kurar ama header'da bugünkü üç sayfa içi
   bağlantıyı (`#how`, `#security`, `#pricing`) tutar; menü ve footer'ın "Product" sütunu **tek bir
   tablodan** çizilir ve yalnız bağlı rotaları listeler (LP-02'de yalnız "API docs"). Her sayfa kartı
   (LP-06…LP-10) rotasını bağlarken o rotaya giden **her** bağlantıyı da ekler: nav girdisi
   (`aria-current` ile), menü, footer, landing'deki "more"/fits bağlantısı.
2. **Raporun LP-11'i kaldırıldı** (işi sayfa kartlarına dağıldı) ve kartlar yeniden numaralandı:
   **LP-11 = Signup/Legal/API kabuğu** (raporun LP-13'ü). Raporun numaraları öneriydi; ID'ler ilk kez
   bu dosyada veriliyor ve ledger'da henüz satır yok, yani "ID yeniden kullanılmaz" kuralına değen
   bir numara yok.
3. **Olgu pinleme altyapısı LP-06'da** (ilk yeni sayfa): `claimedFacts`/`declaredFacts`/render
   yardımcısı ve URL tablosu genelleştirilir; LP-07…LP-10 yeniden kullanır (README §7).
4. **404 (LP-12) kabuk sonlandıktan sonra** (LP-11) — K4 "yeni landing kabuğuyla" diyor.
5. **OG (LP-13) bütün dokuz sayfa var olduktan ve kabuk paylaşılan `marketingDocument`'e son kez
   dokunduktan sonra** — iki kartın aynı kabuk dosyasını aynı anda değiştirmemesi için.
6. **LP-14 yeni: States-Public taraması.** Rapor L09'u LP-01'in CSS'ine katmıştı; odak halkası ve
   temel durumlar LP-01'de kalır, ama "her public kontrol, her durumda" ancak bütün sayfalar kabukta
   olduğunda (LP-11'den sonra) uçtan uca doğrulanabilir.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı**
  bir denetçi kabul kriterlerini tek tek doğrular. Bulgu → düzeltme → **yeni** denetçi. Onay gelmeden
  kart `done` olmaz.
- **Mercek sayısı** (hızlandırma kararı, "küçük işte tek mercek"): yalnız görsel/metin işi → **tek
  mercek**; rota, CSP, handler, kabuk yapısı, test sözleşmesi ya da script değişimi → **iki mercek**
  (kabul + ikinci bir gözle olgu/güvenlik). Kart başlığında yazılı. Tek mercekli kartlarda da denetçi
  olgu pinlerini (README §7) okur.
- **Testler:** tur içinde yalnız değişen testler `-run` ile (ör. `go test ./internal/handler -run
  'Landing|Marketing'`) + saniyelik kapılar (`gofmt`, build, `bash scripts/redline-check.sh`). Tam
  `-race`, `go test ./...`, `make check` bir kartta **bir kez**, commit'ten önceki son doğrulamada.
  Yapıcı ve denetçi brief'ine bu yazılır.
- **`make gen` + `make css`** her `.templ` / `input.css` değişikliğinden sonra; üretilen dosyalar
  commit edilir.
- **Test sözleşmesi** değişirse test **aynı commit'te**, niyeti korunarak güncellenir; hiçbir iddia
  gevşetilmez. Henüz var olmayan bir testin adı belgeye yazılmaz (`TestEveryNamedTestExists`) — yeni
  testi tarif et, adını yapıcı koyar.
- **Bir test silinir ya da adı değişirse eski adın her atfı aynı commit'te güncellenir** — Go
  yorumları, `input.css`, `landingview.go`, tasarım README'si (§11), bu dosya ve depodaki her metin
  dosyası (`grep -rn '<eski ad>' .` ile bulunur). Kabul: `TestEveryNamedTestExists` yeşil ve
  `danglingBudget` (`cmd/tappa/testnames_test.go`, bugün 59) **değişmeden** — eşitlikle denetlenen bir
  cırcırdır; `known-dangling-citations.txt`'ye satır eklenerek geçilmez.
- **Golden'lar değişmez:** `TestUnbrandedScreens_AreByteIdenticalToTheGolden` ve
  `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` bu faz boyunca bayt-aynı kalır; kırmızıya dönerse
  kart paylaşılan bir kabuğa/bileşene dokunmuştur ve düzeltilecek olan koddur.
- **Dal:** `lp-redesign`. `main`'e birleştirme bir deploy kararıdır — kullanıcı istemedikçe yok.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu (orkestratör).

## Her kartın miras aldığı kısıtlar (özet — tam metin README §4)

`marketingCSP` (`default-src 'none'; style-src 'self'; font-src 'self'; form-action 'self';
base-uri 'none'; frame-ancestors 'none'`) — yeni sayfalar **betiksiz**, `img-src` yok: `<img>`, CSS
arka plan görseli, `data:` URI yok; çizim ve ikonlar satır içi SVG · **satır içi `style=` yok**, pano
`style=` → `input.css`'in `.lp` bloğunda sınıf; veriye bağlı değerler sabit sınıf kovaları · fontlar
self-host (Google Fonts bağlantısı taşınmaz), Space Grotesk / IBM Plex Mono yalnız 400/700 · palet
token'ları (+ alfa), ham hex yok · kullanıcıya görünen marka Taptime · her ziyaretçiye aynı bayt,
`public, max-age=300`, çerez/DB okuması yok · GET **ve** HEAD · sayısal/garanti/üstünlük iddiası yok,
`/v1` bağlıyken "API yok" cümlesi yok · footer'daki §4.1/§4.2 cümlesi R1'in tanıdığı ifadeyle, tek
satırda · dokunma hedefi ≥ 44 px, durum yalnız renkle değil, tek `<h1>`, `#main`'e atlama bağlantısı ·
kırılımlar: taban = telefon, 768 = tablet, 1200 = masaüstü; 390 / 834 / 1200 / 1440'ta yatay sayfa
kaydırması yok · Tailwind yalnız `.templ` ve `web/static/js`'i tarar · **her cümle bir olguya pinli**
(README §7), doğrulanmamış iddia (README §10) ya pinlenir ya yumuşatılır.

## Görev listesi

| ID | Faz | Görev | Mercek | Büyüklük | Bağımlılık |
|---|---|---|---|---|---|
| LP-01 | P0 | CSS temeli: `.lp` token'ları, bileşen sınıfları, odak halkası, kırılımlar | 1 | M | — |
| LP-02 | P0 | Ortak public kabuk (landing'de): header, telefon menüsü, `.mbar`, footer; bağlantı tablosu | 2 | M | LP-01 |
| LP-03 | P1 | Landing hero: plaket + telefon maketi + docket, animasyonsuz; `landing.js` + demo diyaloğu stili | 2 | M | LP-02 |
| LP-04 | P1 | Landing gövdesi: How it works, Security, fits, Pricing, FAQ (yalnız görünüm) | 1 | M | LP-02 |
| LP-05 | P1 | Landing'e "The panel" bölümü (Pass/Spike/Roll minyatürleri) | 2 | M | LP-04 |
| LP-06 | P2 | `/pricing` + yeni sayfaların olgu/URL altyapısı + nav testinin yeniden yazımı | 2 | L | LP-04 |
| LP-07 | P2 | `/security` (+ `#proofs`, K2) | 2 | L | LP-06 |
| LP-08 | P2 | `/product` (+ "Take the product tour →") | 2 | L | LP-06, LP-05 |
| LP-09 | P2 | `/for/restaurants` (+ landing fits kartı bağlantısı, K1) | 2 | M | LP-06 |
| LP-10 | P2 | `/for/production` (+ landing fits kartı bağlantısı, K1) | 2 | M | LP-06 |
| LP-11 | P3 | Signup, Legal ve API kılavuzu yeni ortak kabuğa | 2 | M | LP-06 … LP-10 |
| LP-12 | P3 | Public 404 (K4) | 2 + güvenlik | M | LP-11 |
| LP-13 | P3 | Paylaşım kartları: sayfa başına meta + 9 PNG (K3) | 2 | M | LP-11 |
| LP-14 | P3 | States-Public taraması: her public kontrol, her durumda, üç genişlikte | 1 | S | LP-11, LP-12 |
| LP-15 | P4 | Faz sonu: tam `make check`, `make audit`, güvenlik denetimi, skill/README toparlaması | denetçi + güvenlik | M | hepsi |

P2 kartları (LP-07 … LP-10) LP-06'dan sonra birbirinden bağımsızdır (LP-08 ayrıca LP-05'i bekler:
"Take the product tour →" o bölümde durur). Bunu doğru kılan **koşullu kabul**dür (denetim
bulgusu ORTA-1 üzerine seçildi): bir sayfa kartı nav, menü ve footer için **yalnız o ana kadar bağlı
rotaları** ister — header'ın üç girdisinden her biri, sayfası bağlıysa o sayfaya, değilse landing
bölümüne (`/#how`, `/#security`) gider; menü ve footer'ın "Product" sütunu bağlı sayfaları listeler.
Son hâl (header'da Product · Security · Pricing sayfa bağlantıları, menü ve footer'da altı girdinin
hepsi) **LP-11'in** kabulünde bir kez ölçülür; LP-11 LP-06 … LP-10'un hepsine bağlıdır. Neden açık
bağımlılık değil: P2 kartlarını sıraya dizmek (LP-07 → LP-08 → LP-09 → LP-10) hiçbir test ya da
kod gereği olmadan paralelliği kaldırırdı; nav tek tablodan çizildiği için her sıra aynı son hâle
varır. Her sayfa kartı üç genişliği birlikte, **tek DOM** ile taşır.

**Sayfa kartlarının ortak kabulü (LP-06 … LP-10):** rota `GET` ve `HEAD` 200, `marketingCSP`,
`public, max-age=300`; sayfa `marketingURLs()`'te (README §7 adım 4) ve oradan bütün public tarayıcı
testlerinde yeşil (`TestMarketing_EveryInternalLinkResolves`, `TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse`,
`TestMarketing_AnswersHeadWithNoBodyAndTheSameHeaders`, `TestMarketing_ReachesNoThirdParty`,
`TestMarketing_TheSameBytesForEveryVisitor`, `TestLanding_MakesNoUnmeasuredNumericClaim`,
`TestLanding_ShowsNoUserFacingInternalName`, `TestLanding_ClaimsNoAbsentAPIWhileV1IsMounted`,
`TestLandingDemo_NoOtherScreenOnThePublicSurfaceCarriesTheDialog`); üstünlük tarayıcısı
(`superiorityClaim`) sayfaya uygulanır; sayfanın her cümlesi bir pin taşır ve **kendi URL'sinde**
render edildiği ölçülür; kapanış testleri (`TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed`,
`TestLandingSources_EveryDeclaredSourceIsDerivedAndClaimed`, `TestLandingAudiences_EveryClaimRestsOnAProductAnchor`)
yeşil; `TestLanding_UsesOnlyPaletteTokens`'ın kapsamı sayfanın templ dosyasına ve render'ına
genişletilir (ham hex yok, `style=` yok); header'da sayfanın nav girdisi `aria-current="page"` (varsa),
menüde ve footer'da sayfanın bağlantısı (ve yalnız o ana kadar bağlı rotalar — yukarıdaki koşullu
kabul); telefonda `.mbar`; tek `<h1>`; 390 / 834 / 1200 / 1440'ta
yatay kaydırma yok; README §10'daki ilgili iddialar pinli ya da yumuşatılmış; `TestEveryNamedTestExists`
yeşil.

---

## P0 — Temel ve kabuk

## LP-01 — CSS temeli

- **Bağımlılık:** — · **Mercek:** 1 · **Büyüklük:** M
- **Kırmızı çizgi:** — (yalnız stil)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `style(landing): add the public site tokens, components and focus ring`
- **Spec:** [README §5, §6](../design/landing-redesign/README.md) · **Panolar:** `States-Public`, `M-States-Public` (+ her panonun `<style>` bloğu)

**Amaç.** Panoların ortak sınıflarını `input.css`'in `.lp` bloğuna (`input.css:1714-1992`) sınıf
olarak koymak; sonraki kartlar yalnız işaretleme yazsın.

**Neden.** Her pano aynı ~40 sınıfı taşır (README §6); sayfa kartlarında tekrar tekrar karar
verilirse sapar.

**Kapsam.**
- `.lp` token'ları: palet + `--mint`; kullanılmayan `--ink-soft`/`--green-deep` yalnız hiçbir kural
  okumuyorsa silinir (ölç).
- Bileşenler (README §6 tablosu): düğmeler (yeşil, ghost, header düğmesi), nav bağlantısı + current,
  `details.menu` + menü bağlantısı + current, `.mbar`, footer sütunları/bağlantıları, bantlar
  (paper/ink/porcelain), kart, bütün-kart bağlantısı, ikon karosu, "more" bağlantısı, `details.qa`,
  eyebrow/etiket/mono, tap-docket, kaşe eğimleri ve küçük kaşe, `.chk` + tick. Ürünün sahip olduğu
  adlarla çakışanlar `lp-` önekli ya da kapsamlı (bugünkü `.lp-btn`, `.lp-docket` deseni); her ad
  `input.css`'te aranır.
- **Odak halkası:** `.lp :focus-visible` 3 px saffron (`input.css:1799`) → 2 px ink, 2 px ofset;
  `.band--ink` ve yeşil zeminde paper.
- `html:has(.lp) { scroll-padding-top: 72px }`; telefonda `.mbar` payı.
- Kırılımlar: yeni sınıflar taban/768/1200 ile yazılır; mevcut bölümlerin max-width sorguları kendi
  bölüm kartlarında taşınır (LP-03…LP-05).

**Dosyalar.** `web/static/css/input.css` (`.lp` bloğu); gerekirse `internal/handler/marketing_test.go`
/ `marketing_facts_test.go`'da CSS okuyan iddialar.

**Kabul kriterleri.**
- Yeni sınıfların hepsi `.lp` altında ve `@layer` dışında; hiçbir yeni seçici `.lp` dışına uzanmaz
  (bir tarama ile ölç: bloğun her seçicisi `.lp` ya da `html:has(.lp)` ile başlar).
- Odak halkası kontrastı: ink/porcelain 14.32:1, ink/paper 16.17:1, paper/green 7.73:1, paper/ink
  16.17:1 — hesaplanarak doğrulanır (≥ 3:1).
- Basılabilir her yeni sınıf ≥ 44 px (`min-height`).
- Landing bugünkü haliyle görsel olarak bozulmaz (yeni sınıflar henüz kullanılmıyor; değişen tek görünür
  şey odak halkası) — 390/1440 ekran görüntüsü önce/sonra.
- `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`, `TestCompiledCSS_StampWordIsInk` yeşil.

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|TestCompiledCSS_StampWordIsInk'`
(`TestCompiledCSS_StampWordIsInk` `internal/handler/result_test.go:1631`'de),
`go test ./internal/brand -run 'TestCompiledCSS_BrandNamesOccurOnlyInTheGolden'`.

**Tuzaklar.** Ürünün `.btn`/`.docket`/`.stamp`/`.card` adlarını `.lp` altında yeniden tanımlamak
(kalan özellikler sızar — `.lp-docket`'in var olma sebebi) · kaşe kelimesini renkli yapmak (kelime
her zaman ink) · bloğu `@layer components` içine taşımak (ağaç sarsılır) · 500 ağırlığı eklemek.

## LP-02 — Ortak public kabuk (landing'de)

- **Bağımlılık:** LP-01 · **Mercek:** 2 · **Büyüklük:** M
- **Kırmızı çizgi:** §4.1, §4.2 (footer cümlesi)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): add the shared public header, phone menu and footer`
- **Spec:** [README §3, §4, §6](../design/landing-redesign/README.md) · **Panolar:** her panonun header/menü/footer'ı; `M-Landing` (menü, `.mbar`), `States-Public` (durumlar)

**Amaç.** Panoların header'ını (wordmark, nav, Sign in, Start free), telefonun JS'siz `<details
class="menu">` menüsünü, yapışkan alt `.mbar`'ı ve footer'ı (marka + §4.1/§4.2 cümlesi + Product
sütunu + Legal sütunu) **tek bir paylaşılan templ bileşeni** olarak yazıp landing'e takmak.

**Neden.** Beş yeni sayfa ve (LP-11'de) Signup/Legal/API aynı kabuğu kullanacak; bugün landing'in
kendi `landingNav`/`landingFooter`'ı (landing.templ:110, :439) ve öteki sayfaların
`marketingChromeWith`'i (:635) var.

**Kapsam.**
- **Bağlantı tablosu** (tek kaynak, `pages` paketinde): header'ın üç girdisi + menü/footer "Product"
  sütununun altı girdisi (Product tour, Security, Pricing, For restaurants & bars, For production &
  facilities, API docs). **Bu kartta** header girdileri bugünkü sayfa içi bağlantılardır (`#how`,
  `#security`, `#pricing` — `pages.LandingNav`, landingview.go:404); menü ve footer yalnız bağlı
  rotaları listeler (bugün: API docs). Legal sütunu `pages.LegalPages`'ten.
- Header: masaüstü/tablette nav bağlantıları; telefonda `<details class="menu">` (açıkken "Close"),
  içinde menü bağlantıları + API docs; Sign in (`adminLoginPath`) ve Start free (`signupPath`) view'dan.
- `.mbar`: telefonda yapışkan "Start free" (Signup hariç her public telefon sayfası — kabuk bir
  seçenekle kapatabilir).
- Footer: marka, §4.1/§4.2 cümlesi `marketingChrome`'un bugünkü ifadesiyle (landing.templ:564-573'teki
  gerekçe — R1), "© … Taptime · Malta" ve slogan, iki sütun; emoji yok.
- Landing'in eski `landingNav`/`landingFooter`'ı kaldırılır; `.lp` kökü kabuğu sarar.

**Dosyalar.** `web/templates/pages/landing.templ`, yeni kabuk templ'i (ör. `publicchrome.templ`),
`landingview.go` (tablo), `web/static/css/input.css`, `internal/handler/marketing_facts_test.go`
(nav testi gerekiyorsa), `marketing_test.go`.

**Kabul kriterleri.**
- `TestLandingNav_EveryLinkPointsAtASectionOnThePage` **değişmeden** yeşil: ilk `<nav>…</nav>` header
  çubuğudur ve üç sayfa içi bağlantıyı, `signupPath`'i ve `adminLoginPath`'i taşır (Sign in ve Start
  free ilk `<nav>`'ın **içinde** kalmalı ya da test bu kartta niyeti korunarak güncellenir); legal
  sayfaları hâlâ landing nav'ı ve `sticky` taşımaz (kabuk henüz yalnız landing'de).
- `TestMarketing_EveryInternalLinkResolves` yeşil — menü/footer'da bağlı olmayan rota yok.
- Telefon menüsü JS'siz açılır/kapanır; `script-src` landing'de hâlâ yalnız `landing.js`
  (`TestMarketing_PolicyNamesWhatThePageLoadsAndNothingElse`).
- Footer §4.1/§4.2 cümlesi R1'den muafiyet eklemeden geçer (`bash scripts/redline-check.sh` temiz).
- Header ve menü `aria-label`'lı; menü bağlantıları ≥ 48 px, öteki ≥ 44 px; odak halkası LP-01'inki.
- `TestLanding_UsesOnlyPaletteTokens`, `TestLanding_CarriesTheSlogan`, `TestLanding_OffersTheWizardItMounts` yeşil.
- 390 / 834 / 1200 / 1440: yatay kaydırma yok; `.mbar` son içeriği örtmez.

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|Legal|APIGuide'`.

**Tuzaklar.** Header'ı masaüstü ve telefon için iki ayrı DOM olarak yazmak (bağlantılar iki kez —
tek `<details>` menü + CSS ile göster/gizle; tek DOM) · bağlı olmayan bir rotaya bağlantı koymak (O8) ·
footer cümlesini iki satıra bölmek (R1 satır yereldir) · kabuğu bu kartta legal/signup/API'ye takmak
(LP-11).

---

## P1 — Landing

## LP-03 — Hero: plaket, telefon maketi ve docket

- **Bağımlılık:** LP-02 · **Mercek:** 2 (gönderilen betik ve test sözleşmesi değişir) · **Büyüklük:** M
- **Kırmızı çizgi:** —
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): draw the hero as the plaque, the phone and the docket`
- **Spec:** README §2 O2, §6 · **Panolar:** `Landing`, `T-Landing`, `M-Landing`, `LandingDemo`, `M-LandingDemo`

**Amaç.** Hero sahnesini panodaki gibi kurmak: plaket (`#pPlaque`), telefonda tap ekranı maketi
(`#pName`/`#pLoc`), eğik docket + `stamp--approved`; metin aynı (`LandingHeroLede`, `LandingHeroFoot`).
Animasyon yok (O2). Demo diyaloğunun içeriği aynı, yalnız stili ve açılış animasyonu kalkar.

**Kapsam.**
- `landingHero`/`landingScene` (landing.templ:145, :196) yeniden. **Silinecek animasyonlar (blok
  sınırıyla):** `.lp` CSS'i iki bloktur — referans bloğu `input.css:1714-1992` ve demo diyaloğu bloğu
  :1993-2062. Hero'nun `tapring`/`printout`'u (:1862-1868, `.lp .scene.play …`), onların `reduce`
  bloğundaki iki kuralı (:1988 `animation: none`, :1989 `clip-path: none`) ve `class="scene play"`'in
  `play`'i (landing.templ:197) silinir; **docket'i varsayılan gizleyen `clip-path: inset(0 0 100% 0)`
  (`.lp .lp-docket`, :1866) da kaldırılır** — bugün docket'i yalnız `printout` ya da `reduce`
  bloğundaki `clip-path: none` açıyor; ikisi silinip bu bildirim kalırsa hero docket'i hiç görünmez; demo
  diyaloğunun `demoIn`/`demoFade`'i (:2054-2062) **da** silinir — panolarda animasyon yok (öneri, O2),
  ve hareket kalmayınca azaltılmış hareket kuralına da gerek kalmaz. `html:has(.lp)`'nin
  `scroll-behavior` `reduce` kuralı animasyon değildir, kalır.
- **`docket-print`'e dokunulmaz.** `@keyframes docket-print` (`input.css:972-977`, `@layer
  components` içinde) bugün yalnız sihirbazın `signup.templ:583`'ü tarafından kullanılıyor (ölçüldü:
  landing'de yok). `TestLanding_ReducedMotionLeavesTheDocketVisible` onu korur ve bu kartta
  **değişmeden yeşil** kalır; yorumundaki "the landing page's ONE animation" ifadesi
  (`input.css:948`) eskimiştir — düzeltilirse yalnız yorum, testin adı ve atfı (:965) aynı kalır.
- `web/static/js/landing.js`: döndürme `pName`/`pLoc`/`pPlaque`'ı da doldurur; animasyon sınıfı
  eklemez; "KM Facility" → **"KM Plant"** (`landing.js:21`; `seed.sql:171`). Betik yoksa sayfa ilk
  kaydı statik gösterir (bozulmaz).
- Kırılımlar: tablet 2 kolon (1.1fr/.9fr), telefon tek kolon (README; *gap report*).
- Demo diyaloğu (landing.templ:481): yalnız sınıflar; içerik, etiketleme ve klavyeyle kapanma aynı.
- **`TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse`'un animasyon iddiası tersine
  çevrilir** (orkestratör kararı, panolara uy): bugün derlenmiş CSS'te
  `@media (prefers-reduced-motion:no-preference){.lp .demoDialog[open]{animation:` metnini şart koşuyor
  (`marketingdemo_test.go:165-168`); demo animasyonu kalkınca bu iddia "derlenmiş CSS'te
  `.demoDialog` için hiçbir `animation` yok" olur. Testin öteki iddiaları (etiket, klavyeyle kapanma,
  `.demoClose` 44×44) **aynen** kalır. Testin adı animasyon söylemediği için değişmesi gerekmez;
  değişirse "Çalışma modu"ndaki atıf kuralı (README §11, bu dosya, yorumlar aynı commit'te).

**Dosyalar.** `web/templates/pages/landing.templ`, `web/static/js/landing.js`, `input.css`,
`internal/handler/marketing_test.go`, `marketingdemo_test.go`.

**Kabul kriterleri.**
- `TestLanding_ReducedMotionLeavesTheDocketVisible` değişmeden yeşil (bu kart onun koruduğu
  `docket-print`'e dokunmaz).
- **Yeni test** (adını yapıcı koyar): `input.css`'in iki `.lp` bloğunda (:1714-2062 aralığı — ya da
  yapıcının sınırları yorum işaretlerinden okuduğu hâli) hiçbir `animation` bildirimi ve hiçbir
  `@keyframes` yok, landing'in render'ında `play` sınıfı yok; KONTROL: bloğa bir `@keyframes` ya da
  `animation:` eklenmiş mutasyon kırmızı.
- Bu kart bir testi silerse ya da adını değiştirirse "Çalışma modu"ndaki atıf kuralı uygulanır;
  `TestEveryNamedTestExists` yeşil, `danglingBudget` değişmeden.
- `TestLandingDemo_TheDialogIsLabelledAndCanBeClosedWithoutAMouse` animasyon iddiası **tersine
  çevrilmiş** olarak yeşil ("diyalogda animasyon yok"; etiket, klavye ve 44 px iddiaları değişmeden);
  KONTROL: `.lp .demoDialog[open]`'a bir `animation` geri eklenmiş mutasyon kırmızı.
- **Hero docket'i animasyonsuz görünür** (niyet: docket sınıfı ya da çizimi değişse de hero'nun
  docket'i varsayılan hâlde görünür): yeni test (adını yapıcı koyar) derlenmiş CSS'te hero docket'inin
  sınıfına uygulanan hiçbir kuralda gizleyen `clip-path` (`inset(… 100% …)`), `opacity: 0`,
  `visibility: hidden` ya da `display: none` olmadığını okur; KONTROL: `.lp-docket`'e (ya da yeni
  sınıfa) `clip-path: inset(0 0 100% 0)` geri eklenmiş mutasyon kırmızı. 390 ve 1440 ekran
  görüntüsünde docket görünür.
- `TestLandingDemo_TheScriptDegradesInsteadOfBreaking`, `TestLandingDemo_TheSecondHeroButtonSaysWatchDemoAndOpensTheDialog`,
  `TestLandingLines_EveryLedeIsRenderedAndRestsOnAFact`, `TestLanding_CarriesTheSlogan` yeşil.
- `landing.js`'in döndürdüğü örnek kayıtlarda seed'de olmayan lokasyon adı yok (bir test ya da
  denetçinin ölçümü: dört kaydın `loc`'u `seed.sql`'deki lokasyon adlarından).
- Maket ve plaket SVG/HTML; `<img>` yok; `style=` yok.
- 390 / 834 / 1200 / 1440'ta yatay kaydırma yok.

**Testler.** `go test ./internal/handler -run 'Landing|LandingDemo|Marketing'`.

**Tuzaklar.** `docket-print`'i hero'nunki sanıp silmek (sihirbazın docket'i) · animasyonu silip
docket'i gizleyen `clip-path`'i bırakmak (görünmez docket) · animasyonu `reduce` bloğunda kapatarak
"korumak" ·
maketteki saat/isimleri ürün metni sanmak (örnek veri) · betiğe `fetch`/konum okuması eklemek
(`FactPositionReadOnlyOnPress` betikleri tarar).

## LP-04 — Landing gövdesi (yalnız görünüm)

- **Bağımlılık:** LP-02 · **Mercek:** 1 · **Büyüklük:** M
- **Kırmızı çizgi:** —
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `style(landing): restyle how it works, security, fits, pricing and the FAQ`
- **Spec:** README §6 · **Panolar:** `Landing`, `T-Landing`, `M-Landing`

**Amaç.** How it works (emoji → `.card` + SVG ikon), Security (dört docket + gizlilik kutusu), fits
(iki kart — **bağlantısız**, LP-09/10 bağlar), Pricing (fiyat kartı — "Pricing in detail →" **yok**,
LP-06 ekler), FAQ (`details.qa`, ilk soru açık, `+`/`×`). Trust şeridi, ledger ve CTA korunur.

**Kapsam.** Yalnız işaretleme ve sınıflar; **hiçbir metin değişmez** (her değer `landingview.go`'dan
geliyor). Bölümlerin eski max-width sorguları taban/768/1200'e taşınır.

**Dosyalar.** `web/templates/pages/landing.templ` (`landingHow` :242, `landingSecurity` :311,
`landingFits` :335, `landingPricing` :363, `landingFAQ` :388), `input.css`.

**Kabul kriterleri.**
- Landing'in render edilmiş metni (`renderedLandingText`) bu kartın öncesiyle **aynı** (boşluk
  normalize edilerek) — metin taşınmadı, yalnız görünüm.
- Bütün landing pin testleri yeşil (`TestLandingSteps_EveryStepIsRenderedInOrder`,
  `TestLandingEvidence_EveryPieceIsRendered`, `TestLandingFAQ_EveryQuestionIsRenderedWithItsAnswer`,
  `TestLandingPricingIncludes_EveryLineIsRenderedAndRestsOnAFact`, `TestLandingComparison_EveryRowIsRenderedAndRestsOnAFact`,
  `TestLandingAudiences_EveryClaimIsActuallyRendered`).
- FAQ `<details>`, JS'siz; ilk öğe `open`; özet ≥ 44 px.
- Yeni bağlantı yok (O8).

**Testler.** `go test ./internal/handler -run 'Landing'`.

**Tuzaklar.** "Daha iyi okunsun" diye bir cümleyi düzeltmek (metin işi değil) · fits kartına bağlantı
eklemek.

## LP-05 — "The panel" bölümü

- **Bağımlılık:** LP-04 · **Mercek:** 2 (test sözleşmesi değişir: `landingLines()`/`claimedFacts()`
  genişler, yeni Fact türetmesi ve negatif kontrolü, U2/U3/U10'un ilk yayını — "Çalışma modu"nun
  mercek kuralı) · **Büyüklük:** M
- **Kırmızı çizgi:** —
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): add the panel section`
- **Spec:** README §7 · **Panolar:** `Landing`, `T-Landing`, `M-Landing`

**Amaç.** "Every clock-in lands on The Pass." bölümü: Pass / Spike / Roll minyatürleri (örnek veri),
kısa açıklamalar. "Take the product tour →" **bu kartta yok** (LP-08 ekler).

**Kapsam.** Bölümün her cümlesi `landingview.go`'da bir `Line` (ya da benzeri) + en az bir pin;
minyatürlerdeki isim/saat örnek veri ve iddia taşımaz; Roll telefonda kişi başına 7 kare, Pass 2
docket (README; *gap report*).

**Dosyalar.** `landing.templ`, `landingview.go`, `input.css`, `marketing_facts_test.go`
(`landingLines()` ya da yeni değerin okunduğu yer).

**Kabul kriterleri.**
- Yeni her cümle render edilir ve pinlidir; `TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed`
  yeşil; yeni Fact sabiti eklendiyse türetmesi var ve `TestFactMechanisms_SayNoWhenTheFactIsAbsent`'e
  bir negatif kontrol satırı eklenir.
- README §10'daki ilgili iddialar (U2 "Pick any earlier day", U3 "A note can go…", U10 "Nobody can
  decide…") bu bölümde kullanılıyorsa pinli ya da yumuşatılmış.
- Kaşeler ürünün `stamp--approved/--flagged/--rejected` sınıfları, kelime ink.
- Yeni bağlantı yok.

**Testler.** `go test ./internal/handler -run 'Landing'`.

**Tuzaklar.** Minyatürü "canlı" diye anlatmak (`live` sözcüğü yasaklı ifadelerde — "live headcount",
"live dashboard") · minyatür verisini seed'den okur gibi göstermek.

---

## P2 — Yeni sayfalar

## LP-06 — `/pricing` ve yeni sayfaların altyapısı

- **Bağımlılık:** LP-04 · **Mercek:** 2 · **Büyüklük:** L
- **Kırmızı çizgi:** — (fiyat CLAUDE.md §6: para float değil)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): add the pricing page`
- **Spec:** README §3, §7, §10 (U4, U6) · **Panolar:** `Pricing`, `T-Pricing`, `M-Pricing`

**Amaç.** `/pricing`'i bağlamak ve ilk yeni sayfayla birlikte **bütün yeni sayfaların kullanacağı**
altyapıyı kurmak.

**Kapsam.**
- **Altyapı (README §7):** public sayfa tablosu (Mount ve testlerin okuduğu tek liste; `marketingURLs()`
  oradan türer); `claimedFacts`/`landingLines` bütün public sayfaların değerlerini sayar;
  `declaredFacts` yeni sabitleri görür; render yardımcısı URL alır; `TestLanding_UsesOnlyPaletteTokens`
  ve `superiorityClaim` taraması yeni sayfalara genişler.
- **Sayfa:** fiyat (`publishedPricePerEmployeeMonth`), ücretsiz dönem (`foundingFreeMonths`), örnek
  (`exampleTeamMonthly` — türetilir, yazılmaz), "Pricing locked for life" (`docs/handoff.md:277`
  — *gap report*), "Frozen" fiş kaşesi (O7: `stamp stamp--approved` + FROZEN), Pricing FAQ.
  U4 ve U6 (README §10) pinlenir ya da önerilen yumuşak metne çevrilir.
- **Bağlantılar (O8):** header'ın "Pricing" girdisi `#pricing` → `/pricing` (`aria-current` bu
  sayfada); "How it works" ve "Security" girdileri her sayfada çözülsün diye `/#how`, `/#security`
  olur; menü + footer'a Pricing; landing fiyat kartına "Pricing in detail →".
- `TestLandingNav_EveryLinkPointsAtASectionOnThePage`'in **ilk yarısı** niyeti korunarak yeniden
  yazılır: her nav girdisi ya landing'de var olan bir `id`'ye giden `/#…` ya da 200 dönen bağlı bir
  rota; geçerli sayfanın girdisi `aria-current="page"` taşır, öteki sayfaların taşımaz (adı
  değişirse yeni adı yapıcı koyar). İkinci yarı (legal sayfaları sade) LP-11'e kadar aynen kalır.
- **`TestMarketing_EveryInternalLinkResolves` (`marketing_test.go:759-810`) bu kartta değişir.**
  Bugün her `href`'i olduğu gibi ister; `#` ile başlamayan bir bağlantıyı (`/#how`, `/#security`,
  LP-07'nin `/security#proofs`'u) chi'ye yol `/#how` olarak gider ve **404** döner (LP-00
  denetçisi ölçtü: `httptest.NewRequest("GET", "/#how", nil)` yolu `/#how` bırakıyor). Yeni
  davranış: fragman `#`'ten ayrılır; yol GET ile **200** dönmeli **ve** fragman boş değilse hedef
  sayfanın render'ında `id="<fragman>"` bulunmalı. KONTROL mutasyonları (testin yanında, adını
  yapıcı koyar): landing'de olmayan bir `id`'ye giden `/#yok` bağlantısı **kırmızı**; bağlı olmayan
  `/yok#how` **kırmızı**; doğru `/#how` yeşil. Anti-vakuite eşiği ("followed N link(s)") gevşemez.
- Bu kart `TestLandingNav_EveryLinkPointsAtASectionOnThePage`'i yeniden adlandırırsa (adı artık
  "sayfa içi" demeyecekse) eski adın her atfı aynı commit'te güncellenir: `landingview.go:394`,
  `marketing_facts_test.go:968`, tasarım README'si §11, bu dosya (LP-02, LP-06, LP-11) — "Çalışma
  modu"ndaki kural; `TestEveryNamedTestExists` yeşil, `danglingBudget` değişmeden.
- Rota `admincookiepath_test.go` türetmesinin okuyabileceği biçimde bağlanır (README §3).

**Dosyalar.** `internal/handler/marketing.go` (Mount, handler), yeni `web/templates/pages/pricing.templ`
+ `pricingview.go`, `landingview.go` (tablo/sabitler), `landing.templ`, `input.css`,
`internal/handler/marketing_test.go`, `marketing_facts_test.go`, `marketing_claims_test.go`,
gerekirse `admincookiepath_test.go`.

**Kabul kriterleri.** Sayfa kartlarının ortak kabulü ve:
- Fiyat, ücretsiz ay ve örnek sayfada **türetilmiş** değerlerden; `TestLanding_PriceMatchesTheSchemaItIsCharged`,
  `TestLanding_ExampleTeamCostIsThePriceTimesTheHeadcount`, `TestLanding_FreeMonthsMatchTheFunctionThatGrantsThem`
  yeni sayfayı da kapsar (ya da eşdeğeri yeni sayfa için eklenir).
- KONTROL (altyapının gözü açık): yalnız `/pricing`'de kullanılan bir Fact'in cümlesi silinince kapanış
  testi kırmızı; tablodan çıkarılan sayfanın rotası hâlâ bağlıyken URL testi kırmızı.
- Görsellerde fiyat yok, fiyat yalnız metin.
- `TestEmployeeRoutes_DerivationIsNotVacuous` yeşil.
- `TestMarketing_EveryInternalLinkResolves` fragmanlı bağlantıları yukarıdaki gibi çözer ve üç
  KONTROL'ü geçer (ikisi kırmızı, biri yeşil).

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|Pricing|EmployeeRoutes'`.

**Tuzaklar.** Örnek tutarı ("€90") templ'e yazmak · kaşeye yeni bir kelime sınıfı icat etmek ·
`/#how`'ın httptest'te çözüldüğünü sanmak (çözülmez — fragman yola girer, 404; yukarıdaki test
değişikliği şart) ·
nav'ı `/pricing` için ikinci kez kopyalamak.

## LP-07 — `/security`

- **Bağımlılık:** LP-06 · **Mercek:** 2 (güvenlik iddiaları kodla karşılaştırılır) · **Büyüklük:** L
- **Kırmızı çizgi:** §4.1, §4.2, §4.4, §4.5, §4.7 — sayfa bunları **anlatır**; her cümle kodla pinli
- **Araç:** yapıcı, skill `tappa-brand`, `tappa-sun` (SUN cümleleri)
- **Commit:** `feat(landing): add the security page`
- **Spec:** README §7, §10 (U1, U8, U9) · **Panolar:** `Security`, `T-Security`, `M-Security`

**Amaç.** Dört kanıt (`#proofs`), karar tablosu (beşi gösterilen guardrail + baseline), barınma, oturum,
webhook imzası; landing'in Security bölümüne "How each proof works →" (`/security#proofs`).

**Kapsam.** Var olan `Source` sabitleri (landingview.go:150-190) yeniden kullanılır; U1 (`cmacHexLen`,
`internal/sun/params.go:34`), U8 (K2: "Hosted in the EU (Germany)", kaynak `deploy/README.md:6800-6801`),
U9 ("eleven" — guardrail listesinden türet) pinlenir; 150 m / 60 sn `config.go` varsayılanlarından;
256-bit `internal/session/token.go`'dan; `X-Taptime-Signature` `internal/webhook/signing.go`'dan
(*gap report* satırları). `.chk` her zaman glif taşır. Bağlantılar (O8): nav "Security" → `/security`,
menü, footer, landing "How each proof works →".

**Dosyalar.** `marketing.go`, yeni `security.templ` + `securityview.go`, `landingview.go`, `landing.templ`,
`input.css`, `marketing_claims_test.go`, `marketing_facts_test.go`, `marketing_test.go`.

**Kabul kriterleri.** Sayfa kartlarının ortak kabulü ve:
- Sayfada "Falkenstein", "fsn1", "Hetzner" yok (bir test iddiası); "Hosted in the EU (Germany)" var.
- Sayılar (16, eleven/11, 150, 60, 256) templ'de yazılı değil, türetilmiş değerden basılır; KONTROL:
  kaynak değer değişince sayfa testi kırmızı.
- `#proofs` id'si sayfada var; landing'in `/security#proofs` bağlantısı LP-06'da fragman ayıracak
  biçimde değişen `TestMarketing_EveryInternalLinkResolves` ile çözülür (yol 200 **ve** `id` sayfada);
  LP-06'nın bu değişikliği yoksa ya da geri alınmışsa bu kart başlamaz. KONTROL: `#proofs` id'si
  kaldırılmış mutasyon kırmızı.
- §5'in QR cümlesi ("a position on its own is not enough" niyeti) ve satır 6'nın ayrık yapısı
  landing FAQ'daki iddialarla çelişmez (`TestLandingFAQ_EveryAnswerRestsOnAProductFact`'in yasaklı
  ifadeleri bu sayfaya da uygulanır).

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|Security'`.

**Tuzaklar.** "more secure than" türünden üstünlük cümlesi · CMAC'i "16-byte" yazmak (yayınlanan 8
bayt = 16 hex karakter) · guardrail sayısını elle yazmak.

## LP-08 — `/product`

- **Bağımlılık:** LP-06, LP-05 · **Mercek:** 2 · **Büyüklük:** L
- **Kırmızı çizgi:** §4.3 ("Nothing here can be edited" — transactions değişmez), §4.6
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): add the product tour`
- **Spec:** README §2 O1, §7, §10 (U2, U3, U5, U7, U10) · **Panolar:** `Product`, `T-Product`, `M-Product`

**Amaç.** Ürün turu: "On this page" (gerçek `id`'lere düz anchor — panodaki `href="#"` yer tutucu),
The Pass / Spike / Roll, People, Venues & plaques, Rules (guardrail'ler), Integrations; landing'in
"The panel" bölümüne "Take the product tour →".

**Kapsam.** Kopya `NEW.prod`, `NEW.people`, `NEW.venues`, `NEW.plaques`, `NEW.guards`, `NEW.integ`
(README §1) — her cümle pinli. O1: "Taptime guarantees" başlığı ve maddesi yeniden yazılır (öneri:
"Taptime's guardrails"). U2, U3, U5, U10 pinlenir ya da README §10'daki yumuşak metne çevrilir.
Bağlantılar (O8): nav'ın ilk girdisi "How it works" `/#how` → **"Product"** `/product` (panodaki
etiket); menü + footer "Product tour"; landing "Take the product tour →".

**Dosyalar.** `marketing.go`, yeni `product.templ` + `productview.go`, `landingview.go`, `landing.templ`,
`input.css`, `marketing_facts_test.go`, `marketing_test.go`.

**Kabul kriterleri.** Sayfa kartlarının ortak kabulü ve:
- `guarantee` sözcüğü hiçbir biçimde render edilmez (`TestLanding_MakesNoUnmeasuredNumericClaim`
  regex istisnası **olmadan** yeşil).
- "On this page" bağlantılarının her biri sayfadaki bir `id`'ye çözülür (test).
- Davet cümlesi (U5) iki teslim kipini de doğru söyler ya da hiçbirini iddia etmez.
- Header nav'ının "Product" girdisi `/product` ve bu sayfada `aria-current="page"`; öteki iki girdi
  sayfaları o ana kadar bağlıysa sayfalarına, değilse landing bölümlerine gider (koşullu kabul —
  "Görev listesi" altındaki not); LP-06'da yeniden yazılan nav testi yeşil.

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|Product'`.

**Tuzaklar.** Board'un `NEW` nesnesindeki her cümleyi aynen almak (yarısı başka sayfaların) ·
"edit"/"delete" vaadi (§4.3) · Integrations'da "no API" demek (`/v1` bağlı).

## LP-09 — `/for/restaurants`

- **Bağımlılık:** LP-06 · **Mercek:** 2 · **Büyüklük:** M
- **Kırmızı çizgi:** —
- **Araç:** yapıcı, skill `tappa-brand`, skill `tappa-seed` (tablo ↔ seed)
- **Commit:** `feat(landing): add the restaurants page`
- **Spec:** README §2 K1, §7 adım 5 · **Panolar:** `ForRestaurants`, `T-ForRestaurants`, `M-ForRestaurants`

**Amaç.** Zincirler sayfası: gece vardiyası (Rusty Bar örneği, yön "son açık girişe göre" —
`AnchorDirectionFollowsLastOpenEntry`), şube listesi (K1), şube başına saatler; landing fits'in
"Multi-location" kartı bütün-kart bağlantısı olur.

**Kapsam.** Şube tablosu `test/fixtures/seed.sql` ile aynı (K1; *gap report* :139-172) — bir test
seed'i okuyup karşılaştırır; "from our demo data" etiketi kalır. Anlatı cümleleri `LandingAudiences`
anchor'larını yeniden kullanır. Bağlantılar: fits kartı, menü, footer.

**Dosyalar.** `marketing.go`, yeni `forrestaurants.templ` (ya da iki For sayfası için ortak bir templ) +
view, `landing.templ` (`landingFits` :335), `input.css`, testler.

**Kabul kriterleri.** Sayfa kartlarının ortak kabulü ve: şube adları/saatleri seed'den sapınca test
kırmızı (KONTROL); fits kartının tamamı tek `<a>`, içinde başka etkileşimli öğe yok; odak halkası kartı
sarar.

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|For'`.

**Tuzaklar.** Örnek saatleri seed'e "uydurmak" (seed değişmez; sayfa seed'i izler) · gece vardiyasını
takvim gününe göre anlatmak (§5).

## LP-10 — `/for/production`

- **Bağımlılık:** LP-06 · **Mercek:** 2 · **Büyüklük:** M
- **Kırmızı çizgi:** — (debounce cümlesi §5: kişi bazlı, 60 sn varsayılan)
- **Araç:** yapıcı, skill `tappa-brand`, skill `tappa-seed`
- **Commit:** `feat(landing): add the production page`
- **Spec:** README §2 K1, §7 · **Panolar:** `ForProduction`, `T-ForProduction`, `M-ForProduction`

**Amaç.** Tek tesis, çok departman: departman vardiyası (`AnchorDepartmentShift`,
`AnchorLatenessFollowsDepartment`), kapıda sıra (debounce **kişi** bazlı), KM Plant departman tablosu
(K1; *gap report* seed.sql:198-211); landing fits'in ikinci kartı bağlantı olur.

**Kabul kriterleri.** Sayfa kartlarının ortak kabulü ve: departman tablosu seed'den (KONTROL ile);
"60 seconds (the default window)" config varsayılanından türetilir; menü ve footer'ın Product
sütunu bu sayfayı ve **o ana kadar bağlı** öteki sayfaları listeler (koşullu kabul — altı girdinin
hepsi LP-11'de ölçülür).

**Dosyalar ve testler.** LP-09'un eşi.

**Tuzaklar.** Debounce'u etiket bazlı anlatmak · "KM Facility".

---

## P3 — Kabuğun yayılması, 404, paylaşım, durumlar

## LP-11 — Signup, Legal ve API kılavuzu yeni ortak kabuğa

- **Bağımlılık:** LP-06 … LP-10 · **Mercek:** 2 · **Büyüklük:** M
- **Kırmızı çizgi:** §4.1/§4.2 (footer cümlesi)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(landing): move signup, legal and the API guide onto the public chrome`
- **Spec:** README §5 (⚠️ `.lp *`), §11 · **Panolar:** `States-Public` (bu sayfaları kabukla çizer); panel panoları `Signup`, `Legal`

**Amaç.** `marketingChromeWith` (landing.templ:635) yerine LP-02'nin kabuğu: Signup (`.mbar` **yok**),
dört legal sayfası, `/docs/api` ve `/docs/api/tr`. Gövdeler değişmez.

**Kapsam.**
- Bu sayfaların gövdeleri Tailwind; README §5'in uyarısı: `.lp *` sıfırlaması Tailwind boşluklarını
  ezer (kaynak sırasından çıkarım, tarayıcıda ölçülmedi) → gövde `.lp` köküne **alınmaz**; kabuk kendi
  kökleriyle (header, footer) çalışır — önce ölç, sonra seç.
- `TestLandingNav_EveryLinkPointsAtASectionOnThePage`'in **ikinci yarısı** (legal sayfaları landing
  nav'ı ve `sticky` taşımaz, marketing_facts_test.go:1015-1028) niyeti korunarak **tersine çevrilir**:
  bütün public sayfalar aynı nav'ı taşır, legal sayfasında hiçbir nav girdisi `aria-current` değil
  (geçerli belge kendi gezinmesinde işaretli — `TestLegalPages_NavigationMarksTheCurrentDocumentAndWhatIsNotInForce`).
- Signup: `TestSignupSurface_UsesNoUnanchoredCapabilityWord` footer'ın her sözcüğünü de okur
  (landing.templ:559-563 yorumu) — "report", "export" vb. çıkarsa ya pin ya sözcük değişir.
- API kılavuzu: `marketingChrome`'un "bayt-aynı" geçmişi (M12-46) bilinçli olarak biter;
  `TestAPIGuidePages_AreTheGuideInTheMarketingChrome` niyeti korunarak güncellenir.

**Dosyalar.** `web/templates/pages/{signup,legal,apiguide}.templ`, `landing.templ`
(`marketingChrome*` emekliye), `input.css`, `marketing_facts_test.go`, `marketing_test.go`,
`apiguidepage_test.go`, `signup_test.go`.

**Kabul kriterleri.**
- On üç sayfada (/, beş yeni, signup, dört legal, iki kılavuz) tek kabuk bileşeni; header/menü/footer
  bağlantıları aynı tablo.
- **Son hâl (P2'nin koşullu kabulünün kapanışı):** header'ın üç girdisi sayfa bağlantısıdır
  (Product `/product`, Security `/security`, Pricing `/pricing`) ve hiçbiri `/#…` değil; menü ve
  footer'ın Product sütunu panodaki altı girdinin hepsini panodaki sırayla taşır (Product tour,
  Security, Pricing, For restaurants & bars, For production & facilities, API docs).
- Nav testinin ikinci yarısı tersine çevrilirken testin adı değişirse "Çalışma modu"ndaki atıf kuralı
  uygulanır; `TestEveryNamedTestExists` yeşil, `danglingBudget` değişmeden.
- Signup'ta `.mbar` yok; ötekilerin telefon görünümünde var.
- `TestSignupSurface_UsesNoUnanchoredCapabilityWord`, `TestSignupSurface_EverySignInLinkGoesToTheSignInPage`,
  `TestSignupSurface_EveryCapabilityClaimHoldsToday`, `TestAPIGuidePages_FitAPhone`,
  `TestAPIGuidePages_AreLinkedFromTheFootersAndThePanel`, `TestAPIGuidePages_ServedOnTheProductHostOnly`,
  `TestLegalPages_SkeletonsAreNotIndexedAndSayWhatTheyAreWaitingFor`, `TestLegalPublicPath_WritesNothing` yeşil.
- Legal gövdelerinin ve kılavuz makalesinin aralıkları önce/sonra ekran görüntüsünde aynı (390, 1440).
- Signup'ın CSP'si (`signupCSP`, signup.go:953) değişmez.

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|Legal|APIGuide|Signup'`.

**Tuzaklar.** Signup'a `.mbar` (pano: Signup hariç) · gövdeyi `.lp`'ye sarıp Tailwind boşluklarını
kaybetmek · `marketingChrome`'u silip `TestLayoutShells_EveryOneIsActuallyRendered`'ı unutmak (layout
tarafında kabuk değişmiyorsa etkilenmez — ölç).

## LP-12 — Public 404 (K4)

- **Bağımlılık:** LP-11 · **Mercek:** 2 + agent `tappa-security-auditor` · **Büyüklük:** M
- **Kırmızı çizgi:** §4.5 dolaylı (operatör yüzeyi ayrılığı, ADR 0020) · marka/logo varlık gizliliği
- **Araç:** yapıcı, skill `tappa-brand`, güvenlik denetçisi
- **Commit:** `feat(landing): answer unknown public paths with a page`
- **Spec:** [README §8](../design/landing-redesign/README.md) · **Panolar:** yok (kabuk + tek cümle)

**Amaç.** Müşteri host'unda bilinmeyen bir yola, yeni kabukla, tek cümle + "Back to the home page"
bağlantısı taşıyan 404 sayfası.

**Kapsam.**
- README §8'in envanteri **ölçülerek** tamamlanır: router'ın düz 404'üne bayt-eşit olması gereken her
  yer (`apihost.go`, `operatorhost.go`, tema/logo retleri ve testleri, erişim günlüğü).
- Önerilen biçim (README §8): yalnız `GET`/`HEAD`, yalnız müşteri host'u, ret yanıtları router'la
  özdeş kalması gereken öneklerin (**envanterden**: `/brand/`, `/static/`, `/operator`, `/v1`, tap ve
  panel önekleri …) **dışında**; geri kalan her 404 `http.NotFound`.
- **Tema testleri ve 404'ün nereye bağlandığı.** `brandtheme_test.go` müşteri host'unda
  `/no/route/is/mounted/here`'i **dört** yerde referans alır (:89, :512, :550, :602; :712 operatör
  host'unda) ve router'ını `httpx.NewRouter(nil, nil, NewBrandTheme())` ile kurar — **Marketing'i
  bağlamaz** (:64, :704). Bu yüzden:
  - 404 **Marketing'in `Mount`'unda** bağlanırsa bu dört test **değişmez** — ama artık üretim
    bileşimini anlatmazlar (üretimde Marketing bağlı). Kart o zaman tam bileşim üzerinde
    (`cmd/tappa`'nın uygulama router'ı) yeni bir test ekler (adını yapıcı koyar): müşteri host'unda
    bilinmeyen bir tema dosyası ile `/brand/` altındaki yönlendirilmemiş bir yol bayt-aynı yanıt alır.
  - 404 **router seviyesinde** (`internal/httpx`) bağlanırsa dördü de değişir: referans yolları
    `/brand/` içindeki yönlendirilmemiş bir yola taşınır (tema rotası `/brand/theme/{file}`,
    `brandtheme.go:15`); :712 (operatör host'u) değişmez.
  Hangisi seçilirse seçilsin korunacak özellik aynı: `/brand/…` yoklayan tek yanıt görür. Logo testi (`brandlogo_test.go:545-561`) `/t/logo/…`
  ve `/admin/brand/logo/…` için **sabit** bir router-404 yanıtıyla karşılaştırır; tap ve panel önekleri
  dışlandıkça **değişmez**.
- Durum 404, `marketingCSP`, `noindex`, `Cache-Control` kararı gerekçeli (öneri: `no-store`).
- ADR notu (ADR 0020 ya da 0023'ün yanına, tarihli): hangi 404 nerede, neden.

**Dosyalar.** `internal/httpx/router.go` (ya da marketing'in bağladığı bir `NotFound`), `internal/handler/marketing.go`,
yeni `notfound.templ`, `internal/handler/brandtheme_test.go` (yalnız router seviyesi seçilirse: dört referans yolu),
`cmd/tappa` (Marketing seviyesi seçilirse: tam bileşim testi), yeni testler,
`docs/adr/…`.

**Kabul kriterleri.**
- Müşteri host'unda `GET /bilinmeyen` → 404 + kabuk + tek cümle + `/` bağlantısı; `HEAD` gövdesiz.
- Operatör host'unda bilinmeyen `/operator/…` → bugünkü düz 404 (bayt-aynı); API host'unda her
  bilinmeyen yol → bugünkü yanıt.
- `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders` ve öteki tema testleri yeşil — router seviyesinde
  referansları `/brand/` içine taşınmış, Marketing seviyesinde değişmemiş **ve** tam bileşim testi
  eklenmiş; `TestLogoRoutes_ShapesTheRouteDoesNotMatchGetTheRoutersAnswer` **değiştirilmeden**
  yeşil; KONTROL: `/brand/` (ya da tap/panel) önekinde düz 404 yerine sayfa dönen mutasyon kırmızı.
- `TestAccessLog_UnroutedRequestStillGetsARecord` yeşil (yol günlüğe girmez).
- `tappa-security-auditor` bulgusuz (ya da bulgular düzeltilip yeniden denetlenmiş).

**Testler.** `go test ./internal/httpx ./internal/handler -run 'NotFound|BrandTheme|LogoRoutes|AccessLog|Marketing'`
+ `go test ./internal/handler/operator -run 'Host|Gate'` (operatör paketi yavaş — yalnız ilgili test).

**Tuzaklar.** Global `r.NotFound` (operatör host'una müşteri kabuğu; marka retlerinin referansı
değişir) · 404 sayfasına istek yolunu yazdırmak (yansıtma + günlük) · 404'e `public, max-age` vermek
gerekçesiz.

## LP-13 — Paylaşım kartları: meta + 9 PNG (K3)

- **Bağımlılık:** LP-11 · **Mercek:** 2 + agent `tappa-security-auditor` (üçüncü taraf
  tarayıcısına muafiyet) · **Büyüklük:** M
- **Kırmızı çizgi:** — (üçüncü taraf yok kuralı)
- **Araç:** yapıcı, skill `tappa-brand`, güvenlik denetçisi
- **Commit:** `feat(landing): add page descriptions and share images`
- **Spec:** [README §9](../design/landing-redesign/README.md) · **Panolar:** `ShareCards`

**Amaç.** On üç public sayfaya `description` + `og:title`/`og:description`/`og:image`/`og:type` +
`twitter:card`; dokuz 1200×630 PNG `web/static/og/` altında (dört legal sayfa bir görseli, iki
kılavuz sayfası bir görseli paylaşır — README §9).

**Kapsam.**
- PNG'ler K3'e göre **ShareCards tuvalinden dışa aktarılır** (kullanıcının tasarım aracı), kart başına
  1200×630, ve commit edilir. İkinci bir yol yok: yapıcı dışa aktaramıyorsa kullanıcının
  aktarımını ister — ekran yakalama aracı ya da depoda üreteç yok. Görselde fiyat yok.
- Meta yalnız `marketingDocument` yolunda (base.templ:473); `documentHead`'in paylaşılan çıktısı
  değişmez (`TestUnbrandedScreens_AreByteIdenticalToTheGolden`).
- `og:image` mutlak URL: köken `TAPPA_BASE_URL`'den (`config.go:276`), `NewMarketing`'e verilir
  (`cmd/tappa/main.go:863`). İmza değişikliği **bütün çağıranları** değiştirir (ölçüldü,
  `grep -rn 'NewMarketing(' .`: tanım dışında **10 çağrı satırı** — `main.go:863` ve altı test
  dosyasında 9 satır): `cmd/tappa/serving_test.go:52`, `cmd/tappa/approuter_test.go:221`,
  `internal/handler/marketing_test.go:91` (`marketingRouterFor`) ve `:544`,
  `internal/handler/signup_test.go:981, 983`, `internal/handler/apiguidepage_test.go:212, 251`,
  `internal/handler/operator/op10_db_test.go:319`.
- `TestMarketing_ReachesNoThirdParty`'nin tarayıcısı `content="https://…"`'yi yakalar → **yalnız**
  yapılandırılmış kökenin `/static/og/` URL'lerine dar bir muafiyet (OpenAPI muafiyetinin biçiminde);
  KONTROL: başka bir host'a `og:image` kırmızı.
- Description metinleri ShareCards'ın kaynak gösterdiği `Line` değerlerinden (türetilir, kopyalanmaz);
  `/product`'un açıklaması LP-08'in lede'inden.

**Dosyalar.** `web/templates/layout/base.templ`, `web/templates/pages/*` (çağrılar), `internal/handler/marketing.go`,
`cmd/tappa/main.go`, `web/static/og/*.png`, `NewMarketing`'in yukarıda sayılan çağıranları
(`cmd/tappa/serving_test.go`, `cmd/tappa/approuter_test.go`, `internal/handler/marketing_test.go`,
`signup_test.go`, `apiguidepage_test.go`, `internal/handler/operator/op10_db_test.go`).

**Kabul kriterleri.**
- On üç sayfanın her biri tam bir `description` ve dört `og:*` taşır; `og:image` dosyası embed edilmiş
  ağaçta var ve `/static/og/…` 200 döner (`staticAssetURL` deseni).
- Her PNG 1200×630 (test PNG başlığını okur).
- `TestMarketing_TheSameBytesForEveryVisitor`, `TestMarketing_ReachesNoThirdParty` (dar muafiyetle),
  `TestUnbrandedScreens_AreByteIdenticalToTheGolden`, `TestLayoutShells_EveryOneIsActuallyRendered`,
  `TestAPIGuidePages_NameBothTranslationsInTheHead` yeşil.
- Description'lar §4 kopya kurallarından geçer (sayı/garanti/iç ad).
- `tappa-security-auditor` muafiyetin dar olduğunu doğrular (yalnız yapılandırılmış köken, yalnız
  `/static/og/`, yalnız `og:image`/`twitter:image` içeriği) — bulgusuz ya da düzeltilip yeniden
  denetlenmiş.

**Testler.** `go test ./internal/handler -run 'Marketing|Landing|APIGuide|Unbranded|LayoutShells|Signup'`
+ `go vet ./cmd/tappa ./internal/handler/operator` (çağıranların test dosyaları dahil derlendiğini
saniyeler içinde gösterir) + `go test ./cmd/tappa -run 'Packaging|AppRoute'`.

**Tuzaklar.** Meta'yı `documentHead`'e eklemek (tap/aktivasyon golden'ı) · muafiyeti "her https"
yapmak · görsele fiyat yazmak · PNG'yi 1200×630 dışında dışa aktarmak · bir çağıranı unutmak
(`cmd/tappa` ve `operator` paketleri `./internal/handler` testinde derlenmez).

## LP-14 — States-Public taraması

- **Bağımlılık:** LP-11, LP-12 · **Mercek:** 1 · **Büyüklük:** S
- **Kırmızı çizgi:** —
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `style(landing): give every public control its drawn states`
- **Spec:** README §6 (odak halkası) · **Panolar:** `States-Public`, `M-States-Public`

**Amaç.** Panodaki matris — birincil/ghost/header/CTA düğmesi, nav bağlantısı, Sign in, footer
bağlantısı, "more", FAQ, menü, menü bağlantısı, `.mbar`, bütün-kart bağlantısı × default / hover /
focus-visible / pressed / disabled / current-open — her public sayfada uygulanmış olsun; eksikler
tamamlanır.

**Kabul kriterleri.**
- Matrisin her dolu hücresi ürün CSS'inde bir kurala karşılık gelir (denetçi pano ↔ kural tablosu);
  "n/a" hücreleri için kural yok.
- Disabled yalnız form gönderimlerinde (Signup); ziyaret edilmiş bağlantı rengi yok.
- On üç sayfada (/, beş yeni, signup, dört legal, iki kılavuz) ve 404'te klavyeyle tur: her kontrol
  odak halkası gösterir, halka zeminde ≥ 3:1.
- 390 / 834 / 1440 ekran görüntüsü turu (her sayfa).

**Testler.** `go test ./internal/handler -run 'Landing|Marketing|TestCompiledCSS_StampWordIsInk'` +
`go test ./internal/brand -run 'TestCompiledCSS_BrandNamesOccurOnlyInTheGolden'`.

**Tuzaklar.** Hover'ı telefonda zorunlu sanmak (dokunmada hover yok) · `:visited` rengi eklemek.

---

## P4 — Kapanış

## LP-15 — Faz sonu doğrulaması

- **Bağımlılık:** hepsi · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Kırmızı çizgi:** §4.1, §4.2 (public metin), §4.7 (sızan sır yok), §9 (palet, marka)
- **Commit:** `docs(brand): record the public site chrome and its components` (+ yalnız düzeltme commit'leri)

Tam `make check` (fmt + lint + `-race` test) ve `make audit` **bir kez**; bütün LP diff'i üzerinde
güvenlik denetimi: CSP'ler (landing'in `script-src`'i dışında betik yok, yeni sayfalarda `img-src`
yok), üçüncü taraf yok (og muafiyeti dar), 404'ün host/önek kapsamı, marka/logo ret özdeşliği,
footer'ın §4.1/§4.2 cümlesi, R1 muafiyeti eklenmemiş; her public sayfada "her cümle pinli" kapanış
testleri yeşil ve README §10'un her satırı pinli ya da yumuşatılmış (denetçi satır satır işaretler);
390 / 834 / 1200 / 1440 ekran görüntüsü turu (13 sayfa + 404 + demo diyaloğu + açık menü). Skill
`tappa-brand`'e public kabuk ve `.lp` bileşenlerinin tarihli özeti; tasarım README'sinin §6 kataloğu
gerçekleşen sınıf adlarıyla güncellenir.

**Dosyalar.** `.claude/skills/tappa-brand/SKILL.md`, `docs/design/landing-redesign/README.md`, yalnız
düzeltme gerektiren kod/test dosyaları.

**Kabul kriterleri.** `make check` ve `make audit` (redline-check kısmı) yeşil; `TestEveryNamedTestExists`
yeşil; güvenlik denetçisi bulgusuz ya da bulgular düzeltilip yeni denetçiyle yeniden denetlenmiş.
Bittiğinde `state.md`; `main`'e birleştirme **kullanıcının deploy kararıdır**.
