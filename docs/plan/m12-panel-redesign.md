# M12 — Panel yeniden tasarımı "The Pass": ekran içeriklerinin taşınması

> **Durum:** plan (2026-10-20, 29. oturum). Kabuk ve gezinme zaten koda taşındı (`0bb5085` +
> `dfb122d`, dal `claude/admiring-brown-jw8hf1`). Bu milestone **sayfa içeriklerini** tasarıma taşır.
> Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya "yapıldı" yazılmaz.
>
> **Teslim paketi (okumadan başlama):** [docs/design/panel-redesign/README.md](../design/panel-redesign/README.md)
> — kararlar D1–D4, her ekranın miras aldığı kısıtlar, token haritası, bileşen kataloğu (Tailwind
> tarifleriyle), ekran haritası. Ekran başına ayrıntı: `docs/design/panel-redesign/screens/*.md`
> (`file:line` atıfları HEAD `b284899`'a göredir). Tasarım tuvali:
> <https://claude.ai/artifact/T1t7icoKDSixrxM54QG8LT> (yalnız sahibine açık) ve `docs/design/panel-redesign/boards/`.

## Kullanıcı kararları (2026-10-20) — bağlayıcı

- **D1 — Kod gerçektir.** Port **görünümü** taşır, ürün özelliği eklemez. Panoda olup üründe olmayan
  her şey kaldırılır (liste: README §2 ve [backlog](../backlog.md) → *"Panel redesign — drawn but not
  built"*). Ürünün gerektirdiği her şey (testle sabitlenmiş metin, §4.6 nicelikleri, stok plaket
  listesi, tam uid, parola formu, T125 uyarısı, çıkış bloğu …) pano düşürmüş olsa da kalır. Panonun
  yanlış ürün olguları koda göre düzeltilir (11 guardrail, 9 baseline, takvim günü, `8h 02m`, DB durum
  kelimeleri, en yeni önce, 3 adımlı signup, 3 bedava ay, 20 sn × 2+ gün, GPS yarıçapı yapılandırma …).
- **D2 — Köşeler ve çipler.** Adisyon (docket) ve kaşe **keskin** kalır (`rounded-none`, en çok
  `rounded-sm`); kart, alan, düğme, çip, sayfa yuvarlatılabilir; çipler `rounded-full`. `tappa-brand`
  skill'ine tarihli not düşüldü.
- **D3 — Yeni okumalar: hepsi.** Mevcut tablolarda yeni sqlc sorguları (şema değişikliği yok), her biri
  ayrı kart, `tappa-db-migrator` ile; RLS testi, açık `tenant_id`, kanıt okuyucu / çalışan-lokasyon
  okuyucu envanteri, holding (K-39). **Ölçülmemiş bir şey için asla 0 gösterilmez.**
- **D4 — Gerçek QR.** Venues'taki plaket çizimi plaketin **gerçek** QR'ını (`<base>/t?tag=<uid>`)
  `internal/qrcode` ile sunucuda satır içi SVG olarak çizer; görünüm tipi `[][]bool` taşımaz.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı** bir
  denetçi kabul kriterlerini tek tek doğrular. Bulgu varsa düzeltilir ve **yeni** bir denetçiyle yeniden
  denetlenir; onay gelmeden kart `done` olmaz ve sonrakine geçilmez. §4'e değen kartlarda ayrıca agent
  `tappa-security-auditor`. Her **yeni sorgu** agent `tappa-db-migrator` ile; her UI işi skill
  `tappa-brand` okunarak.
- **Testler:** tur içinde yalnız **değişen** testler `-run` ile + saniyelik kapılar (`gofmt`, build,
  `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...` ve `make check` bir kartta **bir kez**,
  commit'ten önceki son doğrulamada. Yapıcı ve denetçi brief'ine bu yazılır.
- **`make gen`** her `.templ` / `db/queries/*.sql` değişikliğinden sonra; üretilen dosyalar commit edilir.
- **Golden politikası:** holdingless golden'ları (reports, anomalies), key-budget golden'ları (The Pass)
  ve WL-8 panel özetleri yalnız **ayrı bir commit'te** yeniden yazılır; commit mesajı **yazılı bayt
  kontrolünü** taşır (`0bb5085` yöntemi: her sayfanın kabuk kesilmiş kendi işaretlemesi eski golden'la
  karşılaştırıldı; holding / holding'siz çifti ortaklık bölümü dışında özdeş; olmayan bir banner hâlâ 0
  bayt ekliyor). Golden'ı "yeşile çevirmek için" yeniden yazmak yasaktır.
- **Dal:** iş `claude/admiring-brown-jw8hf1`'de (ya da kullanıcının açtığı feature dalında). `main`'e
  birleştirme bir deploy kararıdır — kullanıcı istemedikçe yok.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu.

## Her kartın miras aldığı kısıtlar (özet — tam metin README §3)

CSP `style-src 'self'`: satır içi `style=` yok → sınıf kovaları (genişlik %0–100 21 adım, bant
yükseklikleri, `nth-child` eğimleri) ya da SVG öznitelikleri · Tailwind yalnız `.templ` ve
`web/static/js`'i tarar; sınıf adı Go'da kurulmaz, templ yorumuna yardımcı sınıf adı yazılmaz · yeni JS
yok (yalnız `/admin` htmx taşır) · IBM Plex Mono yalnız 400/700 · `components.Notice`, `components.Card`,
`pages.Problem`, `layout.*` değişmez → `PanelNotice`, `PanelEmpty`, `PanelProblem` · tek `<h1>` ·
`desk` = 1200 px'te içerik kutusu **864 px** · K-39 kanıt gizleme ledger'da, şablonda değil · yasak alan
adları (`ip`, `gps`, `lat`, `addr`, `code`, `key` …) · saat/para float'sız · dokunma hedefi ≥ 44 px ve
`panelTouchTargets` kaydı · durum asla yalnız renkle; metin en az `ink/70` · accent yuvaları büyümez.

## Görev listesi

| ID | Faz | Görev | Ajan | Bağımlılık |
|---|---|---|---|---|
| M12-01 | P0 | Kabuk kancaları: Kicker / Title / Crumbs / Actions, h1 boyu, içerik genişliği | yapıcı (`tappa-brand`) | — |
| M12-02 | P1 | Billing dondurma formu `month` taşımıyor | yapıcı | — |
| M12-03 | P1 | `HasProofOfPlace` okuma tarafı /7 · /31 kuralını uygulamıyor | yapıcı + güvenlik | — |
| M12-04 | P1 | Giriş ekranı kanıt listesi practice tap'ten söz ediyor | yapıcı | — |
| M12-05 | P1 | Sıfırlama metni olmayan bir "başka sahip" akışı vaat ediyor | yapıcı | — |
| M12-06 | P1 | Signup boş giriş href'i geçiyor | yapıcı | — |
| M12-07 | P1 | Signup 3. adım "yöneticilerini sonra davet et" diyor | yapıcı | — |
| M12-08 | P2 | Token / tarif katmanı (`input.css`) + kova yardımcıları + dokunma hedefleri | yapıcı (`tappa-brand`) | M12-01 |
| M12-09 | P2 | `PanelNotice` + `PanelEmpty`; `EmptyState` çağıranları | yapıcı (`tappa-brand`) | M12-08 |
| M12-10 | P2 | `PanelProblem` + `renderProblem` geçişi | yapıcı + güvenlik | M12-08 |
| M12-11 | P2 | Docket varyantları: duyarlı docket, `RailDocket`, `VerdictStamp`, kanal alanı, ctr dolgusu | yapıcı (`tappa-brand`) | M12-08 |
| M12-12 | P2 | Gerçek QR'lı plaket çizimi (D4) | yapıcı (`tappa-sun`, `tappa-brand`) + güvenlik | M12-08 |
| M12-13 | P3 | Okuma: "on the floor now" + lokasyon başına sayım | `tappa-db-migrator` | M12-01 |
| M12-14 | P3 | Okuma: saatlik bant (saat başına vardiyadaki kişi) | `tappa-db-migrator` | — |
| M12-15 | P3 | Okuma: dünden açık kalan girişler (`ListOpenCheckIns` yeniden kullanımı) | `tappa-db-migrator` | — |
| M12-16 | P3 | Okuma: günün verdict sayımları | `tappa-db-migrator` | — |
| M12-17 | P3 | Okuma: durum başına çalışan sayısı | `tappa-db-migrator` | — |
| M12-18 | P3 | Okuma: listede iş unvanı + son dokunuş | `tappa-db-migrator` | — |
| M12-19 | P3 | Okuma: bir kişinin son 7 dokunuşu | `tappa-db-migrator` | — |
| M12-20 | P3 | Okuma: kişi özeti (+ bekleyen davet okuyucusu) | `tappa-db-migrator` | — |
| M12-21 | P3 | Okuma: The Roll — rol + ev lokasyonu, `OpenEntry.EmployeeID`, hücre işaretleri, sayaçlar | `tappa-db-migrator` | — |
| M12-22 | P3 | Okuma: Odd shapes — lokasyon başına kanıt dağılımı | `tappa-db-migrator` + güvenlik | — |
| M12-23 | P3 | Okuma: Odd shapes — çiftlerin gün noktaları | `tappa-db-migrator` | — |
| M12-24 | P3 | Okuma: Odd shapes — plaket sayacı from → to | `tappa-db-migrator` | — |
| M12-25 | P3 | Okuma: The Spike — "bugün karar verilen" sayısı | `tappa-db-migrator` | — |
| M12-26 | P4 | Ekran: The Pass (bugün) | yapıcı (`tappa-brand`) | M12-01, 08, 09, 11, 13, 14, 15 |
| M12-27 | P4 | Ekran: geçmiş gün / ledger | yapıcı (`tappa-brand`) | M12-11, 16, 26 |
| M12-28 | P4 | Ekran: The Spike | yapıcı (`tappa-brand`) + güvenlik | M12-01, 08, 09, 11, 25 |
| M12-29 | P4 | Ekran: Odd shapes | yapıcı (`tappa-brand`) + güvenlik | M12-01, 08, 09, 22, 23, 24 |
| M12-30 | P4 | Ekran: People (liste + kart) | yapıcı (`tappa-brand`) | M12-01, 08, 09, 17, 18, 19, 20 |
| M12-31 | P4 | Ekran: kişi formu (ekle + yönet) | yapıcı (`tappa-brand`) | M12-30 |
| M12-32 | P4 | Ekran: davet sonucu | yapıcı (`tappa-brand`) | M12-09, 20 |
| M12-33 | P4 | Ekran: manuel kayıt | yapıcı (`tappa-brand`) | M12-01, 08, 09, 11 |
| M12-34 | P4 | Ekran: Venues & plaques | yapıcı (`tappa-brand`) | M12-01, 03, 08, 09, 12 |
| M12-35 | P4 | Ekran: lokasyon formu (+ departman formu) | yapıcı (`tappa-brand`) | M12-34 |
| M12-36 | P4 | Ekran: The Roll | yapıcı (`tappa-brand`) | M12-01, 08, 09, 21 |
| M12-37 | P4 | Ekran: Rules | yapıcı (`tappa-brand`) + güvenlik | M12-01, 08 |
| M12-38 | P4 | Ekran: kural değişikliği incelemesi + sürüm sayfası | yapıcı (`tappa-brand`) | M12-37 |
| M12-39 | P4 | Ekran: Billing | yapıcı (`tappa-brand`) | M12-02, 08, 09 |
| M12-40 | P4 | Ekran: ay dondurma | yapıcı (`tappa-brand`) | M12-39 |
| M12-41 | P4 | Ekran: Account | yapıcı (`tappa-brand`) + güvenlik | M12-01, 08, 09 |
| M12-42 | P4 | Ekran: Developers | yapıcı (`tappa-brand`) + güvenlik | M12-01, 08, 09 |
| M12-43 | P4 | Ekran: giriş | yapıcı (`tappa-brand`) | M12-04, 08 |
| M12-44 | P4 | Ekran: işletme seçimi + parola sıfırlama | yapıcı (`tappa-brand`) | M12-05, 08 |
| M12-45 | P4 | Ekran: signup | yapıcı (`tappa-brand`) | M12-06, 07, 08 |
| M12-46 | P4 | Ekran: legal (genel sayfa) | yapıcı (`tappa-brand`) | M12-08 |
| M12-47 | P4 | Ekran: M-More sayfası cilası | yapıcı (`tappa-brand`) | M12-08 |
| M12-48 | P5 | Skill / ADR toparlaması | yapıcı | M12-26 … M12-47 |
| M12-49 | P6 | Son tam `make check` + güvenlik denetimi + golden incelemesi | denetçi + güvenlik | hepsi |

P3 okumaları birbirinden bağımsızdır ve P2 ile paralel yürüyebilir; her ekran kartı yalnız kendi
okumalarını bekler. Her kart tek başına sevk edilebilir (dal üzerinde, CI yeşil).

---

## P0 — Kabuk

## M12-01 — Kabuk kancaları ve içerik genişliği

- **Bağımlılık:** — (kabuk `0bb5085` + `dfb122d` üstünde)
- **Kırmızı çizgi:** §9 (tenant markası yuvaları değişmez)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(panel): let a section own its kicker, title, crumbs and actions`

**Amaç.** `pages.PanelChrome`'a isteğe bağlı **Kicker** (mono üst satır), **Title** (bölüm etiketinin
yerine geçer), **Crumbs** (`[]Crumb{Label, Href}`) ve **Actions** (başlığın sağındaki çocuk) yuvaları;
`panelChrome` bunları basar, belgede yine **tek `<h1>`**.

**Neden.** Her panonun başlığı (kicker + 44/36/30 px başlık + sağda eylemler; alt sayfalarda
"New person", "Freeze September 2026", "Review this change") bugünkü kabukla kurulamıyor
(`web/templates/pages/admin.templ:428-430`). Her analist aynı açığı buldu; bir kez yapılır.

**Kapsam.** Yuvalar; h1 `text-3xl md:text-4xl desk:text-[44px] leading-none tracking-[-0.035em]`;
Crumbs bir `nav aria-label="Breadcrumb"`; `.panel-well` genişliği yeniden ele alınır (bugün `max-w-5xl` =
1024 px; `desk` = 1200'de içerik kutusu 864 px, 1440'ta 1104 px — tavan ~72 rem'e çıkabilir, bütün desk
tarifleri 864 px'e sığmalı). Bölümler henüz yuvaları kullanmaz (boş yuva = bugünkü çıktı).

**Dosyalar.** `web/templates/pages/admin.templ`, `adminview.go`, `web/static/css/input.css`;
`internal/handler/panelbrand_golden_test.go` ve golden'lar (ayrı commit).

**Kabul kriterleri.**
- Yuvası boş her bölüm h1 boyutu dışında bayt-aynı; h1 boyutu değişimi WL-8 panel özetlerinde ve
  holdingless / key-budget golden'larında **ayrı bir commit'te**, yazılı bayt kontrolüyle yeniden yazılır.
- Her bölümde tam bir `<h1>` (Title verilince Title, yoksa bölüm etiketi); Crumbs varken son öğe
  `aria-current="page"`.
- `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`'in `layout.*` girdileri değişmez; yapısal
  iddiaları korunur.
- `TestPanelSections_ThePhoneNavMarksExactlyOneCurrentAndOffersEverySection`,
  `TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty` yeşil.
- 390 / 834 / 1200 / 1440'ta yatay sayfa kaydırması yok.

**Tuzaklar.** Sayfanın kendi ikinci `<h1>`'i yazması · accent'i başlığa taşımak · yuvaları zorunlu
yapıp her bölümü aynı committe değiştirmek.

---

## P1 — Mevcut kusurlar (her biri ayrı, küçük)

## M12-02 — Billing "Freeze <month>" formu ay alanı taşımıyor

- **Bağımlılık:** —
- **Kırmızı çizgi:** — (`billing_periods` append-only; GET yazmaz)
- **Araç:** yapıcı
- **Commit:** `fix(billing): send the month with the page's freeze form`

**Amaç.** Sayfanın kendi dondurma formuna `<input type="hidden" name="month" value={ v.MonthValue }>`.

**Neden.** `billing.templ:149-151`'deki form `month` göndermiyor; `readBillingMonth`
(`billingactions.go:~250-268`) onu istiyor → düğme her zaman `?problem=unreadable`. Testler formu
doğrudan post ettiği için kimse görmedi (`billingactions_test.go:47-77`).

**Kabul kriterleri.** Yeni handler testi: sahip olarak `/admin/billing?month=<biten ay>` render edilir,
**sayfanın kendi formu** ayrıştırılır ve `/admin/billing/close`'a post edilir → 200 ve o ayın onay sayfası
(problem değil). Yönetici yine 403; `TestBillingRead_WritesNothing` yeşil.

## M12-03 — `Venue.HasProofOfPlace()` okuma tarafı kuralını uygulamıyor

- **Bağımlılık:** —
- **Kırmızı çizgi:** §5 (kanıt anlamıyla tanımlıdır; okuma tarafı)
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Commit:** `fix(venues): treat a too-wide address list as no proof of place`

**Amaç.** `internal/domain/tenant/venue.go:316-318` `HasProofOfPlace()` adres listesini yalnız
`len(StaticIPs) > 0 && !netx.TooWideForProofOfPlace(StaticIPs)` iken kanıt sayar.

**Neden.** Bugün geçmişte kaydedilmiş geniş bir liste (`{0.0.0.0/1, 128.0.0.0/1}`) "kanıt var" sayılıyor;
"No proof of place" uyarısı çıkmıyor, panoların "IP ✓" rozeti yalan söylerdi. Yazma kapısı geçmişi
düzeltemez (`transactions` değişmez, CLAUDE.md §5).

**Kabul kriterleri.** Tablo testi: geniş liste + GPS yok → "No proof of place" (sayfada tam bir kez);
geniş liste + GPS → uyarı yok ama IP kanıt sayılmaz; dar liste → kanıt. `TestLocationsSection_SaysWhenAVenueHasNoProofOfPlace`
yeşil. Karar motorunun okuma tarafıyla (`internal/domain/tap/decide.go:667-722`) aynı fonksiyon.

## M12-04 — Giriş ekranı kanıt listesi practice tap'i anlatıyor

- **Bağımlılık:** — · **Araç:** yapıcı · **Commit:** `fix(login): drop the practice-tap claim from the sign-in panel`

**Amaç.** `AdminLoginProof`'un 3. satırı ("The first tap is a practice run…") ADR 0026 ile kalkan bir
davranışı vaat ediyor; satır kaldırılır ya da doğru cümleyle değişir.

**Kabul kriterleri.** `TestAdminLogin_BrandPanelClaimsOnlyWhatTheProductDoes`
(`adminloginsplit_test.go:303-358`) yeni metne bağlanır; "first N months free" iddiası ve yasaklı ifadeler
korunur; sayfa CSP'si ve betik kümesi değişmez.

## M12-05 — Sıfırlama metni olmayan bir akışı vaat ediyor

- **Bağımlılık:** — · **Araç:** yapıcı · **Commit:** `fix(reset): stop promising an owner can set another admin's password`

**Amaç.** `AdminResetRequest` ve `AdminResetSent`'teki *"Ask another owner of your business to set a new
password for you from the panel."* cümlesi kaldırılır; yerine doğru olan (ör. Taptime'a başvurmak) ya da
hiçbir şey.

**Kabul kriterleri.** Kayıtlı / kayıtsız yanıtlar hâlâ bayt-aynı (`TestAdminReset_RegisteredAndUnregisteredAreByteIdentical`,
`TestAdminReset_TimingIsFlat`); e-posta adresi yankılanmaz; `adminreset_test.go` yeşil.

## M12-06 — Signup boş giriş href'i geçiyor

- **Bağımlılık:** — · **Araç:** yapıcı · **Commit:** `fix(signup): link the header's sign-in to the sign-in page`

**Amaç.** `signupShell` `marketingChrome("")` çağırıyor → "Sign in" bulunduğu sayfaya gidiyor. Giriş yolu
geçirilir (Landing ve Legal aynı kabuğu paylaşır; parametre ya da signup'a özel başlık).

**Kabul kriterleri.** Her signup ekranının "Sign in" bağlantısı `/admin/login`'e gider; signup iddia
taramaları (`signup_test.go`) ve `marketing_test.go` yeşil; "sign in" signup iddia sözlüğünde dayanaklı.

## M12-07 — Signup 3. adım yönetici davetini vaat ediyor

- **Bağımlılık:** — · **Araç:** yapıcı · **Commit:** `fix(signup): drop the manager-invite promise from step three`

**Amaç.** *"You can invite the rest of your managers later."* kaldırılır — yönetici daveti yok (M7-07 /
Q02 bloklu).

**Kabul kriterleri.** Cümle hiçbir signup ekranında yok; iddia sözlüğü taraması "invite"ın çalışan
davetine dayanmasıyla bu cümleyi gizlemiyor (denetçi kontrol eder); `signup_test.go` yeşil.

> *Kusur değil (D1):* manuel kayıt The Pass sekmesi altında açılır (`manualentry.go:691`); panolar buna
> göre düzeltildi, kod değişmez.

---

## P2 — Paylaşılan bileşenler

## M12-08 — Token / tarif katmanı, kova yardımcıları, dokunma hedefleri

- **Bağımlılık:** M12-01
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(panel): add the redesign's component classes and class buckets`

**Amaç.** README §4–§5'in paylaşılan sınıfları `input.css` `@layer components` altında: `.panel-card`,
`.stat-card`, `.lbl`, `.field` / `.field-input`, `.chip` / `.chip-group`, `.pill` + tonlar, `.btn--ink`,
`.btn--line`, `.btn--danger`, `.btn--approve`, `.btn--reject`, `.tally--pill`, `.ev-tile*`, `.queue-item*`,
`.roster-row`, `.venue-card*`, `.ev-badge*`, `.roll-cell*`, `.tape-bar--1…10`, `.rail-item:nth-child` eğim
kuralları, `.ladder` / `.rung` / `.plate` / `.lock`, `.switch*`. Kova yardımcıları **tek bir `.templ`
dosyasında**: `widthPct` (21 literal), `tapeBar` (10 literal).

**Kabul kriterleri.**
- Kullanılmayan bir bileşen sınıfı derlenmiş CSS'e girmez (Tailwind temizler) — her sınıf ilk kullanan
  ekran kartıyla canlanır; bu kart yalnız kova yardımcılarını ve kuralları ekler.
- Yeni basılabilir sınıflar `panelTouchTargets`'ta ve derlenmiş CSS 44 px kontrolünde.
- Hiçbir templ yorumunda yardımcı sınıf adı yok; `brand` geçen yeni sınıf adı yok
  (`TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`); `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` yeşil.
- `widthPct` birim testi: 0, 100, 5'e yuvarlama, sıfır güvenli; yığılmış çubuklar için en büyük kalan
  yöntemi toplamı 100 yapar.

## M12-09 — `PanelNotice` + `PanelEmpty` ve `EmptyState` çağıranları

- **Bağımlılık:** M12-08
- **Kırmızı çizgi:** §9 (paylaşılan bileşenler değişmez), §4.6 (boş durum `Queried`'a bağlı)
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(panel): add panel-only notice and empty-state components`

**Amaç.** Yalnız panelin kullandığı `components.PanelNotice(tone, heading)` (ok "Done" / warn "Heads up" /
alert "Not done", `role` doğru) ve `components.PanelEmpty(where, heading, cta)`; `EmptyState` çağıranları
(transactions :71, reports :224, locations :189/215/263/295, review :128, anomalies :205/374, employees
:247, admin :516) yeni bileşene geçer — **metinleri ve dalları değişmeden**.

**Kabul kriterleri.** `components.Notice` / `Card` / `pages.Problem` / `layout.*` baytları değişmez
(`TestUnbrandedScreens_AreByteIdenticalToTheGolden`); holdingless golden'ları **ayrı commit'te** yazılı bayt
kontrolüyle yeniden yazılır; "Nothing here yet" arayan testler önceden grep'lenir; her boş durum hâlâ
`Queried`'a bağlı; "06:00", "Order plaques", "Invite someone" metinleri yok.

## M12-10 — `PanelProblem` ve `renderProblem` geçişi

- **Bağımlılık:** M12-08
- **Kırmızı çizgi:** §9 (`pages.Problem` tap/aktivasyonundur), güvenlik başlıkları
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Commit:** `feat(panel): render panel problems on a panel problem card`

**Amaç.** `pages.PanelProblem(PanelProblemView{Status, OwnerOnly, Title, Message, Hint, CTAHref, CTALabel})`;
`AdminAuth.renderProblem` (`internal/handler/adminlogin.go:1757`) panel problemlerinde ona geçer. Kod çipi
`status`'tan; OwnerOnly problem sabitinden; oturumsuz problemler CTA olarak `/admin/login`'e gider.

**Kabul kriterleri.** `pages.Problem` baytları değişmez; 401 sayfası yok, 404 sayfası yok (chi
varsayılanı kalır); "Your taps are still being recorded" yok; problem başına metin aynı;
`TestAdminScreens_CarryTheSecurityHeaders`, `TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy`,
`TestAccount_AFailedReadIsAProblemPageAndNeverABlankForm`, `TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail`
yeşil (gerekirse kasıtlı güncelleme, gevşetmeden).

## M12-11 — Docket varyantları

- **Bağımlılık:** M12-08
- **Kırmızı çizgi:** K-39 (§9), §5 manuel kayıt ayrı görünür
- **Araç:** yapıcı, skill `tappa-brand`
- **Commit:** `feat(panel): make the docket responsive and add the rail docket`

**Amaç.** Tek duyarlı `TransactionDocket` (`desk`'te tam `dl`, altında kompakt kanıt satırı — iki kopya
değil); yeni `components.RailDocket`; `verdictStamp` → dışa açık `VerdictStamp`; `DocketView.Channel`;
ctr altı haneye dolgulu (`#000641`); `PartnerVenue` için "· partner venue".

**Kabul kriterleri.** Damga ve docket **keskin** (D2); işaretler kodun kelimeleri ("IP yes · GPS no",
"n/a"); yöneticinin notu motor notuyla birleşmez; ortak satırda "not shown at a partner venue" ve
güven/IP/GPS etiketleri yok; manuel ve TRAINING ayırt edilir. `TestTransactionDocket_*`,
`TestTransactionDocket_ShowsTheEngineVerdictAndTheManagerDecisionSeparately`,
`TestHoldingPanel_PartnerViewsSayTheOutcomeNeverTheReason`, `TestTypes_NothingOnTheReadPathCanCarryACoordinate`
yeşil; key-budget golden'ları değişirse ayrı commit.

## M12-12 — Gerçek QR'lı plaket çizimi (D4)

- **Bağımlılık:** M12-08
- **Kırmızı çizgi:** §4.7 (anahtar şekil yürüyüşü), skill `tappa-sun` (URL tek temsil)
- **Araç:** yapıcı (skill `tappa-sun`, `tappa-brand`) + agent `tappa-security-auditor`
- **Commit:** `feat(venues): draw the plaque with its real QR code`

**Amaç.** `components.PlaqueDrawing(PlaqueDrawingView{Last4, QRPath, QRSide})`; QR yolu `internal/qrcode`
(`Encode`, `Bitmap(QuietZone)`) ile ve SVG yol yardımcısı `internal/qrcode`'a (tek uygulama; operatör de
ona geçebilir) ya da components'a kopya (panel `operatorpages`'i import etmez). URL `<base>/t?tag=<uid>`
`sun.MintTapPath`'in yanındaki tek bir yardımcıdan; `?src=qr` yok.

**Kabul kriterleri.** Yeni test: çizilen QR, `internal/qrcode/read_test.go`'daki okuyucuyla çözülünce
`<base>/t?tag=<uid>`; ink on paper; görünüm tipinde dilim yok, `TestPlaqueViewModels_CannotCarryAKey`
bilinçli güncellenir (yeni tip yürüyüşe eklenir); figür `role="img"` + `aria-label`; metin skill'deki gibi
("Hold your phone here", "Or scan — same thing", `PLAQUE 91AC`, "drawing, not to scale"); taban host'u
NDEF şablonuyla (`internal/sun/ndef.go:29`) aynı.

---

## P3 — Yeni okumalar (D3)

Her okuma kartının ortak kabul kriterleri: **(a)** `db/queries/*.sql` + `make gen`; **(b)** açık
`tenant_id = @tenant_id` ve her JOIN'in ON'unda tenant tekrarı; **(c)** RLS DB testi — A'nın bağlantısı B'nin
satırını görmez, test sorgusu **tenant süzgeci olmadan** yazılır (CLAUDE.md §6); **(d)** kanıt sütunu
okuyorsa `internal/db/evidencereaders_test.go`'ya sınıflı giriş, `employees` + `location_id` adlandırıyorsa
`TestEmployeeLocationReaders_Counted`'a giriş; **(e)** holding: ortak satırlar K-39'a göre (kanıt sayısına
girmez; ad yalnız `siteVenuesFor`); **(f)** üç hâl: sorgulanmadı → iddia yok; başarısız → "could not be
read" / "?"; sınırlı → "N+"; sayfa düşmez (savepoint, `inSavepoint` `ledger.go:615-629`); **(g)** sınır
ifadeleri çıplak parametre (`coalesce(@x, …)` yok); **(h)** tohum tenant'ta EXPLAIN, sorgu yorumuna not;
**(i)** `panelLedger` ve benzeri arayüzlerin sahteleri aynı değişiklikte genişler; **(j)** yeni alan adları
yasaklı sözcük içermez. Kırmızı çizgi her birinde §4.5 (+ K-39).

## M12-13 — "On the floor now" + lokasyon başına sayım

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(ledger): count who is on the floor now, per venue`

Kişi başına son açık `in` (`practice` değil, sonrasında `out` yok — `ListOpenCheckIns` /
`GetLastOpenTransaction` yüklemi), `occurred_at >= @since` (`@since = now − tap.StaleOpenIn`, Go'da),
**ayrı kişiler**, `coalesce(location_id, site_location_id)`'ye göre gruplu. İşaretli açık girişler sayılır
(kayıttırlar). Ek kabul: holding iki yön `TestEmployerReaders_NineReadersNameTheSite` ailesine durum olarak
eklenir; büyük sayı ve satırlar aynı okumadan.

## M12-14 — Saatlik bant

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(ledger): count people on shift per venue per hour`

Bugünün takvim gününde geçen saatler için lokasyon başına saatlik kişi sayısı. Yol seçimi EXPLAIN'le:
`ListWorkedShiftEvents` yeniden kullanımı (`[gün başı − StaleOpenIn, şimdi)`, Go'da eşleme; envanterde zaten
`residue`) ya da yeni bir toplama ifadesi — seçim ve ölçüm karta not edilir. Ek kabul: gece taşıması (gece
yarısından önceki `in`), 18 sa'ten eski açık giriş yok sayılır, ortak lokasyon satırları; `row_limit`
aşılınca bant "incomplete" der; gelecek saat yok.

## M12-15 — Dünden açık kalan girişler

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(ledger): count yesterday's check-ins left open`

Yeni SQL yok: `ListOpenCheckIns` (`anomalies.sql:476`) dünün takvim günü (tenant dilimi), `row_limit =
cap+1` ile yeni bir ledger yöntemi. Ek kabul: sınırlıda "N+"; practice satırları hariç (sorgu zaten);
Odd shapes'e bağlantı verisi.

## M12-16 — Günün verdict sayımları

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(ledger): count the day's records by verdict`

`CountPanelTransactionsByVerdict`: `ListPanelTransactions` ile aynı yüklemler (tenant, gün penceresi,
lokasyon, departman, ad CTE'si, kanal), **verdict ve imleç hariç**; verdict başına sayı +
`count(*) FILTER (WHERE channel='manual')`; sayfayla **aynı `WithTenant`** içinde. Ek kabul: `verdict`
okur → envanterde `residue` (ortak satırların verdict'i ADR 0027 §13 madde 5 artığı); "sayım = liste"
testi; ad CTE'si ayrılanları da bulur.

## M12-17 — Durum başına çalışan sayısı

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(roster): count people per lifecycle status`

`CountPanelEmployeesByStatus :many`: `ListPanelEmployees`'in lokasyon/departman/ad yüklemleri, durum hariç,
`GROUP BY status`; listeyle aynı `WithTenant`. Ek kabul: çipler süzgeçleri onurlandırır; kicker toplamı
süzgeçsiz; çalışan-lokasyon envanteri.

## M12-18 — Listede iş unvanı + son dokunuş

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(roster): show each person's job title and last tap`

`ListPanelEmployees`'e `e.role` ve `LEFT JOIN LATERAL (… occurred_at, verdict … ORDER BY occurred_at DESC
LIMIT 1)` (`transactions_tenant_employee_occurred_idx`). Practice hariç. Ek kabul: `verdict` → envanter
girişi; `note`/`trust`/`ip_match`/`gps_*`/`matched_sid` seçilmez; plan indeks taraması kullanır; sıra ve
imleç değişmez (`…PagesThroughEverybodyExactlyOnce` yeşil); bayt tavanı testi yeşil.

## M12-19 — Bir kişinin son 7 dokunuşu

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(roster): read a person's last seven taps`

`ListRecentTapsForEmployee :many` (`occurred_at, type, verdict, channel`, LIMIT 7; isteğe bağlı
`transaction_reviews` sonucu). Ek kabul: envanter girişi; ortak lokasyon satırı yalnız gün/yön/saat/kelime;
`type` NULL → "—".

## M12-20 — Kişi özeti (+ bekleyen davet)

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(roster): read a person's summary for the card`

Ayrı `GetPanelPersonSummary :one` (rol, invited/activated/deactivated_at, canlı oturum sayısı, ilk/son
oturum tarihi) — **`GetPanelEmployeeForAction` genişletilmez** (`Deactivate` / `Move` onu okur). Bekleyen
davet için mevcut `ListPendingInvitesForEmployee` (`invites.sql:302`; kod hash'i yok) bir domain okuyucusuyla
bağlanır. Ek kabul: `device_info`, e-posta, davet kodu görünüm tipine girmez; panelden eklenen davetlide
`invited_at` NULL → tarih uydurulmaz.

## M12-21 — The Roll: rol, ev lokasyonu, hücre işaretleri, sayaçlar

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(ledger): mark other-venue, manual and open days on the roll`

`ListWorkedShiftEvents`'e `e.role`, `e.location_id`, ev lokasyonu adı (tenant'lı LEFT JOIN) — ya da hafta
çalışanları için ayrı küçük okuma (EXPLAIN'e göre). `PersonHours.Role/HomeLocationID/HomeVenue`;
`OpenEntry.EmployeeID`; `accumulate`'te gün başına `Other` / `Manual` / `Open` işaretleri; `Totals.OtherVenue`,
`Totals.ManualArrivals`, `Totals.Approved` (uç noktası `flag` + `approved`). Ek kabul: tablo testleri (başka
lokasyon, manuel, çalışılmış günde açık giriş, işaretliden onaylı, holding eşi "başka lokasyon", gece
vardiyası başlangıç gününde); `ListWorkedShiftEvents` envanterde `residue` kalır; float yok; alan adları
`bannedFieldName`'den geçer.

## M12-22 — Odd shapes: lokasyon başına kanıt dağılımı

- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor` · **Commit:** `feat(anomalies): split each venue's evidence four ways`

`ListAnomalyVenues`'e üç süzgeç — `Both` (`ip_match AND gps_match`), `NetworkOnly` (`ip_match AND NOT
coalesce(gps_match,false)`), `Neither` (biri ölçülmüş, ikisi de eşleşmemiş) — **her biri** `AND
t.site_tenant_id IS NULL`; mevcut `DistanceOnly` ile tanımlar tutarlı ve yorumda yazılı. Ek kabul:
envanterde `site_tenant_id IS NULL` sayısı 3 → 6 ve `why` metni; "every evidence figure leaves the partner
out" testi üç yeni rakama genişler.

## M12-23 — Odd shapes: çiftlerin gün noktaları

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(anomalies): list the days a pair tapped together`

`ListTapsTakenTogether`'e `array_agg(DISTINCT p.local_day ORDER BY p.local_day) AS together_days`;
`Period.DayLabels`'a karşı `[]bool`. Ek kabul: kanıt sütunu yok; görünüm yürüyüşü (`[]bool` izinli) yeşil;
20 sn / lokasyon / 2+ gün tanımı değişmez.

## M12-24 — Odd shapes: plaket sayacı from → to

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(anomalies): show the counter values around the largest gap`

En büyük boşluk için `ctr` (to), `to − gap − 1` (from), o satırın ve aynı `tag_uid`'deki önceki dokunuşun
`occurred_at`'i (pencereden önce olabilir → lateral arama). Ek kabul: `ctr` okur → yeni envanter girişi
(`excluded`, `tag_uid IS NOT NULL` şartı; site satırlarında `tag_uid` NULL, K-30); zamanlar beyan edilen
`occurred_at` — metin "between" der.

## M12-25 — The Spike: bugün karar verilen

- **Araç:** agent `tappa-db-migrator` · **Commit:** `feat(review): count today's decisions`

`CountReviewsSince(tenant_id, since)` `transaction_reviews` üzerinde; `since` = tenant yerel gece yarısı
(`GetTenantClock`). Ek kabul: ledger sorgu ağı açık tenant yüklemini görür; başarısızlık "not counted",
asla 0; `transactions` okumadığı için kanıt envanteri etkilenmez.

---

## P4 — Ekran taşımaları (her kart masaüstü + tablet + telefon birlikte)

Her ekran kartının ortak kabul kriterleri: **(a)** ilgili `screens/*.md` dosyasındaki her satır uygulandı;
"Not ported" listesindeki hiçbir şey çizilmedi; bağlayıcı metin tablosu korundu; **(b)** satır içi `style=`
0, yeni betik 0, `://` href 0; **(c)** o dosyanın test listesindeki testler gevşetilmeden yeşil (kasıtlı
güncellemeler commit mesajında gerekçeli); **(d)** golden değişiyorsa ayrı commit + yazılı bayt kontrolü;
**(e)** dokunma hedefleri ≥ 44 px ve kayıtlı; yeni her renkli metin için AA hesaplandı (ink/50 metin yok);
**(f)** 390 / 834 / 1200 / 1440'ta elle kontrol: yatay sayfa kaydırması yok, desk tarifleri 864 px'e sığar;
**(g)** kova sınıfları literal; **(h)** üç hâlli her rakam "ölçülmedi"yi 0 diye basmaz.

## M12-26 — The Pass (bugün)

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-11, M12-13, M12-14, M12-15
- **Kırmızı çizgi:** §4.3, §4.6, K-39, §4.2 ("live" yok)
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(pass): port The Pass to the redesign`
- **Spec:** [screens/pass-ledger.md §1](../design/panel-redesign/screens/pass-ledger.md)

Kicker (tarih · saat · tam dilim), "Right now" ızgarası (floor + tally, Spike kartı `Pending`'den, left-open
kartı), ray (çipler bağlantı, `RailDocket`'ler, `nth-child` eğim, "See the whole day →", "Show more"
erişilebilir), bant, plaket kartı (`tenant.Plaques.Screen` yeniden kullanımı: monteli, son görülen, ilk 8,
"never", "In service"/"Needs replacing"), plaket bildirimi + key-budget banner'ı "Right now"un üstünde.
`date` yokken Pass, varken ledger. Ek kabul: `/admin` tek betikli sayfa kalır; key-budget golden'ları ayrı
commit; her widget için "sorgulanmamış görünüm iddia etmez" testi; tek `WithTenant` + widget başına savepoint.

## M12-27 — Geçmiş gün / ledger

- **Bağımlılık:** M12-11, M12-16, M12-26 · **Kırmızı çizgi:** §4.3, §4.6, K-39
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(pass): port the past-day ledger to the redesign`
- **Spec:** [screens/pass-ledger.md §2](../design/panel-redesign/screens/pass-ledger.md)

Title "The Pass · Mon 19 Oct", gün adımlayıcı bağlantıları, "Today", süzgeç paneli (+ telefon `<details>`,
gizli `verdict`/`date`), özet satırı + sayılı verdict çipleri, **azalan** saat grupları ("(continued)" ile),
duyarlı docket. Ek kabul: sayfalama her kaydı tam bir kez gösterir (gruplamayla); "Show more" ve boş durum
metinleri kodun; "every offered option is accepted" testi çip bağlantılarını okur.

## M12-28 — The Spike

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-11, M12-25 · **Kırmızı çizgi:** §4.3, §4.6, K-39 (ADR 0027 §13 madde 5(e))
- **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(review): port The Spike to the redesign`
- **Spec:** [screens/spike-odd.md Part S](../design/panel-redesign/screens/spike-odd.md)

`?focus=` seçimi (yalnız yüklenmiş kayıtlar arasından; geçersiz → ilk), sıra en yeni önce, kuyruk
öğeleri, telefon sayfalayıcısı, `SpikeDocket` + kanıt karoları, kısa "why" haritası, `RuleSID` dışa açılır ve
**`nameSiteRecords`'ta ortak satırlar için boşaltılır** (`ledger.go:292-302` yorumu yeniden yazılır), tek
form + iki gönder düğmesi ("Approve" / "Reject"), telefon yapışkan çubuğu, "decided today". Ek kabul:
"Approve — they were there", klavye ipuçları, metre, cinsiyetli zamir yok; ortak satırlı kuyruk durumu
holding testine eklenir; `TestReviewForm_PostsToTheRouteThatIsActuallyMounted` 1 form + odak bağlantılarına
güncellenir.

## M12-29 — Odd shapes

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-22, M12-23, M12-24 · **Kırmızı çizgi:** K-39, §4.3, §4.6
- **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(anomalies): port Odd shapes to the redesign`
- **Spec:** [screens/spike-odd.md Part O](../design/panel-redesign/screens/spike-odd.md)

Kart ızgarası, hafta segmentli kontrolü + GET formu, yığılmış kanıt çubukları (ortak satırda çubuk yok),
çiftler (20 sn / lokasyon / 2+ gün, gün noktaları), sayaç boşlukları (from → to), GPS-only alt kümesi
(eksik olabilir uyarısı), açık girişler ("Close it" yok), kural dağılımı (nötr ton, ortak kova sonda).
Toplamlar, "what this page does not see", kişi listesi, kayıp plaket uyarısı, maskeli sayım, kısaltma
bildirimleri **kalır**. Ek kabul: `fillAnomaliesView`'a enjekte `now`; şablonda `base:`/`sys:` literal yok;
holdingless golden'ları ayrı commit.

## M12-30 — People (liste + kart)

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-17, M12-18, M12-19, M12-20 · **Kırmızı çizgi:** §4.7, §4.6, K-39
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(roster): port People to the redesign`
- **Spec:** [screens/people.md §1](../design/panel-redesign/screens/people.md)

Kicker toplamı, "Add somebody", durum çipleri (DB kelimeleri, sayılı, bağlantı), düz satırlar (baş harf,
ad, rol · lokasyon, hap, son dokunuş, oturum sayısı), tek kart (`?manage=`; desk yan sütun, tablet üstte,
telefonda yalnız kart + "Back to People"), son 7 dokunuş, aktivasyon kutusu (Copy yok), deaktivasyon iki
adım. Ek kabul: satır regex'i aynı commit'te yeni işarete geçer; bayt tavanı yeşil; deaktive kişinin gerçek
oturum sayısı; "Unlink phone", gruplama, "Stopped" yok.

## M12-31 — Kişi formu (ekle + yönet)

- **Bağımlılık:** M12-30 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(roster): port the person form to the redesign`
- **Spec:** [screens/people.md §2](../design/panel-redesign/screens/people.md)

Ekle: aynı URL'de form düzeni, "After you add them" (kipe duyarlı, `InviteByEmail`), telefon yapışkan
gönder. Yönet: aynı kartın tam sayfa düzeni (yeni parametre yok). Ek kabul: `TestEmployeeAdd_*` yeşil;
venuesuz dal sağlam; iki davet düğmesi asla birlikte değil; bağlayıcı yardım metinleri tam; e-posta tam
adres.

## M12-32 — Davet sonucu

- **Bağımlılık:** M12-09, M12-20 · **Kırmızı çizgi:** §4.7 · **Araç:** yapıcı (`tappa-brand`)
- **Commit:** `feat(roster): port the invitation result to the redesign`
- **Spec:** [screens/people.md §3](../design/panel-redesign/screens/people.md)

İsim bloğu, `select-all` kesikli kutu (metin, `<a>` değil), sonraki adımlar (kendi işletmenin plaketi), üç
`Retired` dalı, her e-posta sonucu. Ek kabul: bağlantı yanıtta tam bir kez, log'da yok; "Copy", "Send it
again", "Show a link instead" yok.

## M12-33 — Manuel kayıt

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-11 · **Kırmızı çizgi:** §4.3, §4.7, §6
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(manual): port manual entry to the redesign`
- **Spec:** [screens/people.md §4](../design/panel-redesign/screens/people.md)

Crumbs, "1 Fill in · 2 Check" adımları, radyo kartları (gerçek radyolar), onay adımında gerçek docket
önizlemesi (APPROVED + "Entered by a manager"), tam kalıcılık uyarısı her genişlikte; sekme The Pass kalır;
"when" reddindeki "this venue's own clock" → "this business's own clock". Ek kabul: yazma formu yalnız
`Confirming` iken; başarı yönlendirmesi kaydın kendisine; "Change something" ve 3. adım yok.

## M12-34 — Venues & plaques

- **Bağımlılık:** M12-01, M12-03, M12-08, M12-09, M12-12 · **Kırmızı çizgi:** §5 (kanıt anlamı), §4.7
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(venues): port Venues & plaques to the redesign`
- **Spec:** [screens/venues-roll.md §1](../design/panel-redesign/screens/venues-roll.md)

`?at=` seçimi (yanıtlanan bilinmeyen kimlik), lokasyon kartları (IP rozeti üç hâl), detay (plaket çizimi,
"Where it is", yapılandırmadan yarıçap — `AdminAuth`'a enjekte, `cmd/tappa`'da bağlanır — kanala duyarlı
dipnot, Wi-Fi kanıt dışında), lokasyon plaket tablosu (tam uid, satır başına açma bağlantısı, "In service" /
"Needs replacing"), "In the box", departmanlar + harici ref detayda, bütün bildirimler. Ek kabul: "No proof
of place" sayfada tam bir kez; `?at` sorgu sözlüğü testleri genişler; yarıçap etiketi enjekte değere eşit;
"Order plaques", "Mark lost", pozisyon adı yok.

## M12-35 — Lokasyon formu (+ departman formu)

- **Bağımlılık:** M12-34 · **Kırmızı çizgi:** §5, §6 · **Araç:** yapıcı (`tappa-brand`)
- **Commit:** `feat(venues): port the venue form to the redesign`
- **Spec:** [screens/venues-roll.md §2](../design/panel-redesign/screens/venues-roll.md)

Numaralı bölümler, kanala duyarlı yardım metinleri (F5/F12/F15), ayrıştırıcının cümlesini aynen taşıyan
hata kutusu, tamsayı dakikalı vardiya SVG çubuğu, sağ sütun (güven aritmetiği olmadan docket, bu lokasyonun
departmanları ve plaketleri, harici ref, kaldırma bloğu anlamı değişmeden). Ek kabul: devre dışı kaldırma
düğmesi yok; satır satır okuma ve genişlik başlığı yok; `ACappedListStillNamesItsCountAfterARejectedForm`
geçer ya da gerekçeyle bilinçli yeniden yazılır.

## M12-36 — The Roll

- **Bağımlılık:** M12-01, M12-08, M12-09, M12-21 · **Kırmızı çizgi:** §4.6 (Q18), §6, K-39
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(roll): port The Roll to the redesign`
- **Spec:** [screens/venues-roll.md §3](../design/panel-redesign/screens/venues-roll.md)

ISO hafta kicker'ı, adımlayıcı + "This week" + hafta formu, CSV bağlantısı ve cümlesi, önce fiş (`.docket`,
board'un altı satırı + §4.6 satırlarının hepsi her genişlikte), "Not closed yet." kutusu (Odd shapes'e),
kesme uyarısı ayrı, `.roll-cell*` ızgarası ve açıklaması, telefonda tek DOM akışı, kişi başına tally'ler,
lokasyon listesi, açık giriş listesi, ortak bölüm `templ.Join` ile. Saat biçimi **`8h 02m`**. Ek kabul:
holdingless golden'ları ayrı commit + "holding yalnız ortak bölümü ekler" iddiası; CSV uyum testi değişmez.

## M12-37 — Rules

- **Bağımlılık:** M12-01, M12-08 · **Kırmızı çizgi:** §5 (guardrail'ler kapatılamaz), §4.6, T125
- **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(policies): port Rules to the redesign`
- **Spec:** [screens/rules-billing.md §1](../design/panel-redesign/screens/rules-billing.md)

`.policy-section` ızgarası (intro DOM'da garantilerden hemen önce, desk'te sağ sütunda), 11 guardrail
motorun sırasıyla perçinli plakalarda (yalnız sınıf; SVG yok), domain'de sid → kısa ifade haritası + uyarı
bayrağı (kapsama testiyle), `effectLabel` (bütün etkiler testiyle), numarasız baseline/tenant satırları
(durum çipi, salt-okunur etki hapı, sonucu adlandıran anahtar düğmesi), demirlenmiş varsayılan plaka
garantilerin dışında, yan sütun (dört kanıt, ayardan GPS halkası, ayarlar, GPS notu), yetkiler tam genişlik.
Ek kabul: iki izin listesi **genişlemeden** yeşil; `policyBranchWitness` her yeni dala tanık; yöneticiye hiç
kontrol yok (devre dışı da değil); T125 satırı ve iki problem cümlesi; etki seçicisi ve değişiklik çubuğu yok.

## M12-38 — Kural değişikliği incelemesi + sürüm sayfası

- **Bağımlılık:** M12-37 · **Kırmızı çizgi:** onay MAC'i (bağlanan şey ekranda gösterilen şey)
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(policies): port the rule change review to the redesign`
- **Spec:** [screens/rules-billing.md §2](../design/panel-redesign/screens/rules-billing.md)

İşleme özgü Title, kural adı (`a.rules.Screen`, yeni SQL yok), işlem-dürüst önce/sonra, uyarı çağrısı,
UUID yerine adlar, tam "what nobody can tell you yet" metni, sürüm bağlantıları + güncel sürüm gövdesi.
Ek kabul: gizli alanlar `policyChangeFields` ile bayt-aynı; inceleme yazmaz; izinsiz aktör render'dan önce
reddedilir; tahmini sonuç metni yok.

## M12-39 — Billing

- **Bağımlılık:** M12-02, M12-08, M12-09 · **Kırmızı çizgi:** append-only `billing_periods`, kalemlendirme yok
- **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(billing): port Billing to the redesign`
- **Spec:** [screens/rules-billing.md §3](../design/panel-redesign/screens/rules-billing.md)

Seçili ay kartı (düzen B) ve denklem, "Frozen months" fiş ızgarası (`.docket` + damgalar), plan/kurucu yan
sütunu (aylar `FirstChargeableMonth`'tan), ay seçici + CSV, taban/bedava/kalemlendirme/WhyNotClose,
`month` taşıyan dondurma formu. Ek kabul: yalnız dört faturalama adresi; şablonda yer tutucu, literal "2"/"3"
yok; GET yazmaz; PDF, ödeme kartı, e-posta, "View breakdown" yok.

## M12-40 — Ay dondurma

- **Bağımlılık:** M12-39 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(billing): port the freeze-month review to the redesign`
- **Spec:** [screens/rules-billing.md §4](../design/panel-redesign/screens/rules-billing.md)

Crumbs, kicker, Title, kesik fiş (gruplu `dl` — görünüm modelinde bölünmüş `Detail`), tam metinli
geri alınamaz uyarı, birincil / iptal. Ek kabul: yönlendirme hedefi değişmez (sayfada başarı durumu yok);
formda `month` + token dışında girdi yok; sahip kapısı iki adımda.

## M12-41 — Account

- **Bağımlılık:** M12-01, M12-08, M12-09 · **Kırmızı çizgi:** §9 (accent yuvaları), §4.5, yöneticiye sahip verisi yok
- **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(account): port Account to the redesign`
- **Spec:** [screens/account-auth.md §1](../design/panel-redesign/screens/account-auth.md)

Identity (salt-okunur VAT + dört hâlli çip, 8 tür, dilim girdisi), Your brand (kesikli logo kutusu, seçici
+ hex, yalnız kaydedilen önizleme, mevcut ret/öneri), Password, What your staff read, yalnız sahibe
Developers bağlantı kartı. Ek kabul: `form=` sayısı 0; yöneticide anahtar/webhook verisi yok; "white text"
yok; düzeltilmiş VIES cümlesi; Managers / Group kartı, swatch'lar, oran okuması yok.

## M12-42 — Developers

- **Bağımlılık:** M12-01, M12-08, M12-09 · **Kırmızı çizgi:** §7 (anahtar, sır, URL)
- **Araç:** yapıcı (`tappa-brand`) + agent `tappa-security-auditor` · **Commit:** `feat(apikeys): port Developers to the redesign`
- **Spec:** [screens/account-auth.md §2](../design/panel-redesign/screens/account-auth.md)

Anahtar kartı (satır içi oluşturma, tek seferlik kutu + "I've stored it", desk tablo / altında liste,
iptal tarihi), webhook kartı (ON/OFF, önceden dolu adres, "N of 10 left" `w.Limit`'ten, döndürme tarihi,
onaylı eylemler), teslimler (5 durum, Sending dahil), rehber adresi metin olarak. Ek kabul: anahtar ve sır
kendi yanıtlarında tam bir kez; maskeli `tt_whsec_` ve "ms" yok; `://` href yok; webhook ve sızıntı
testleri yeşil.

## M12-43 — Giriş

- **Bağımlılık:** M12-04, M12-08 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(login): port the sign-in page to the redesign`
- **Spec:** [screens/account-auth.md §4](../design/panel-redesign/screens/account-auth.md)

Yeni marka yarısı (kelime markası bağlantı, statik `aria-hidden` docket'ler, başlık), kart, bildirimler,
parola göster düğmesi değişmeden. Ek kabul: `TestAdminLogin_*` ve `TestAdminLoginPassword_*` aileleri yeşil; betik
kümesi ve CSP aynı; başarısızlık bayt-aynılığı; `layout.Auth` baytları aynı.

## M12-44 — İşletme seçimi + parola sıfırlama

- **Bağımlılık:** M12-05, M12-08 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(login): port business choice and password reset to the redesign`
- **Spec:** [screens/account-auth.md §5–§6](../design/panel-redesign/screens/account-auth.md)

İkisi `layout.Auth(title, "")`'a; Choose'da "Password accepted · one more step", asılı docket'ler,
"Continue to {name}"; Reset'te başlıkta "Back to sign in" (`/new` dahil), statik adım gezintisi. Ek kabul:
numaralandırma ve zamanlama testleri; hiçbir href'te token yok; ikisinde de betiksiz `adminCSP`;
`layout.Auth` yorumu yeni çağıranlara güncellendi.

## M12-45 — Signup

- **Bağımlılık:** M12-06, M12-07, M12-08 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(signup): port signup to the redesign`
- **Spec:** [screens/account-auth.md §7](../design/panel-redesign/screens/account-auth.md)

İlerleme docket'i / telefon ilerleme çubuğu, çipler ve kartlar (yerel radyo + `peer`), 3. adım iki sütun +
Back, VAT tonlu bitti docket'i, problem uyarısı. Ek kabul: dokuz signup ekranının hepsinde tarama yeşil;
honeypot sağlam; "Replay" ve canlı satır ekleme yok.

## M12-46 — Legal (genel sayfa)

- **Bağımlılık:** M12-08 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `feat(legal): port the legal pages to the redesign`
- **Spec:** [screens/account-auth.md §8](../design/panel-redesign/screens/account-auth.md)

Pazarlama kabuğu (panel değil), "Start free" (`SignupHref`), yayın durumu çipli belge gezintisi
(`texts.Published()`), çerez tablosu → telefonda kartlar, numaralı taslak listesi. Ek kabul: çerez ve legal
testleri; bayraklar veriden; önbellek değişmez (her ziyaretçiye aynı bayt).

## M12-47 — M-More sayfası cilası

- **Bağımlılık:** M12-08 · **Araç:** yapıcı (`tappa-brand`) · **Commit:** `style(panel): group the More sheet as drawn`

Yalnız CSS: gruplu yuvarlatılmış liste, oklar, kesikli ayraçlar. Ek kabul: yeni rota yok; rozet sorgusu
yok; Manual entry bağlantısı yok; tek-current testi yeşil.

---

## P5 — Toparlama

## M12-48 — Skill / ADR toparlaması

- **Bağımlılık:** M12-26 … M12-47 · **Araç:** yapıcı · **Commit:** `docs(brand): record the redesign's components and rules`

`tappa-brand` skill'ine yeni bileşen sınıflarının özeti ve D4 notu (panelin plaket çizimi gerçek QR çizer;
tap/aktivasyon ailesi QR'dan hâlâ söz etmez — `TestQRScreens_SayNothingAboutTheQRRoute` kapsamı); ADR 0023
WL-8 ekine (gerekirse) yeni kabuk yuvaları; ADR 0027 §9 okuyucu envanterine eklenen okuyucular; CLAUDE.md
§9'daki okuyucu sayısının teste uyumu; tasarım README'sinin son hâli (bileşen kataloğunun gerçekleşen sınıf
adlarıyla güncellenmesi). Ek kabul: `TestEveryNamedTestExists` yeşil (belgede adı geçen her test var).

## P6 — Kapanış

## M12-49 — Son tam doğrulama ve güvenlik denetimi

- **Bağımlılık:** hepsi · **Araç:** ayrı denetçi + agent `tappa-security-auditor`
- **Commit:** — (yalnız düzeltme commit'leri)

Tam `make check` (fmt + lint + `-race` test), `make audit`, `make check-sandbox` bir kez; bütün M12 diff'i
üzerinde güvenlik denetimi (§4.3 yazma yok, §4.5 her yeni sorgu, §4.6 sıfır-değil-ölçülmedi, §4.7 alan adları
ve sızıntılar, K-39 her yeni okuyucu, CSP, betik, accent yuvaları); golden commit'lerinin bayt kontrolü
yeniden okunur; 390 / 834 / 1200 / 1440 ekran görüntüsü turu. Bulgu → düzeltme → yeni denetçi. Bittiğinde
`state.md`; `main`'e birleştirme **kullanıcının deploy kararıdır**.
</content>
</invoke>
