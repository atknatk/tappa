# ADR 0023 — Tenant markası: bir vurgu rengi + bir logo, sayılı slotlar, co-brand ve CLAUDE.md §9

- **Durum:** kabul edildi — kullanıcı kararı **D-C** (2026-09-24): *"Logo + tap butonu tenant
  renginde; sonuç ekranında yalnız logo (§9 onayı — WL-9 kartına alıntılanır)"*
  ([m10-platform.md](../plan/m10-platform.md) §6) · kullanıcının **iki §9 kararı (2026-10-02)**:
  **K-2a** logosu olmayan ama accent'li tenant'ın tap ekranında başlıktaki `taptime` **ink**
  olur; **K-2b** logo yükleyen tenant'ın tap ve sonuç ekranında üstte logo, altında küçük
  **"taptime · punchless"** co-brand satırı (§7) · White-label **K1** (tek accent + WCAG
  kapısı), **K3** (co-brand — K-2b ile kullanıcı onaylı), **K4** (panelde 4 px şerit), **K6**
  (aktivasyon Taptime + işveren adı), **K8** (e-postada faz 1 yalnız ad) — *"✅ önerisiyle
  uygulanır"* (aynı §6).
  **Uygulama: yok.** Bu ADR yazıldığında (HEAD `c0c0250`) `internal/brand` paketi,
  `tenant_branding` tablosu ve marka rotası yoktur (aşağıda ölçüldü); uygulama WL-1…WL-12.
- **Tarih:** 2026-10-02
- **Bağlam:** [M10 Akış C](../plan/m10-platform.md) §5, görev WL-0. Sapma listesi ve ölçüm
  komutları: aynı dosya → *"Kart düzeltmesi (2026-10-02, WL-0 uygulaması sırasında)"*.
- **İlgili:** [ADR 0024](0024-kullanici-yukledigi-gorsel.md) (logonun kendisi: biçim, sınır,
  yeniden kodlama, saklama, servis) · [ADR 0002](0002-tenant-baglami-ve-rls.md) md.7 ·
  [ADR 0005](0005-kabul-edilen-riskler.md) (marka taklidi — açık ek, aşağıda) ·
  [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) (operatör yüzeyi Taptime kalır) ·
  CLAUDE.md §4.5, §4.6, §9 · skill `tappa-brand` → *"Tenant slotları (taslak)"*

## Neden bir ADR

CLAUDE.md §9 iki cümleyle bu işin önünde durur: *"paletin dışına çıkma"* ve tap ekranı için
*"Bu ekrana özellik eklemek istiyorsan önce sor."* White-label ikisine de dokunur: palete
tenant'ın seçtiği bir renk girer ve tap ekranına bir logo ile bir renk girer. §10 ayrıca şema
(yeni `tenant_branding` tablosu — WL-1) ve güvenlik sınırı (kullanıcının yüklediği görsel —
ADR 0024; yeni rotalar ve sayfa başına CSP) değiştiğinde ADR ister.

Bu ADR **sözleşmeyi** yazar: tenant neyi, nerede, hangi kuralla boyar; neyi boyayamaz. Kod
yazmaz. Normatif kurallar onları uygulayacak WL görevine ve o görevin kabul kriterine
**→** işaretiyle bağlanır.

**Numara notu.** Plan önerilen ADR sırasını *"0022 e-posta taşıyıcısı · 0023 tenant markası/§9 ·
0024 kullanıcı yüklediği görsel"* diye yazdı (m10-platform.md §2). Akış sırası 2026-10-02'de
A1 → **C** → B → A2 oldu (white-label SES'in önüne geçti): kullanıcının *"çok yavaş, paralel
ilerle"* talebi üzerine, SES'in dış adımları kullanıcı tarafından sonraya bırakıldığı için
**orkestratörün önerisi** (otonomi kuralı — önerisi olan kararlar uygulanır ve raporlanır);
2026-10-02'de kullanıcıya raporlandı, itiraz yok. Kullanıcının 2026-09-24 tarihli D-D sırasının
yerine geçer. Numaralar **değişmedi**: 0022 SES
ADR'sine (EM-1) ayrılmış kalır, 0023/0024 ondan önce yazılır. Gerekçe: numaralar planda ve
kartlarda çok yerde atıflı; kaydırmak sarkan atıf üretirdi.

## Bağlam — bugün (ölçüldü, HEAD `c0c0250`)

| Olgu | Kaynak / ölçüm |
|---|---|
| Palet dokuz token; tek kaynak | `tailwind.config.js:26-36` |
| Paletin bağıl parlaklıkları: paper **0,980422** · porcelain **0,862652** · ink **0,013737** · tappa-green **0,083273** | WCAG 2.x formülü, `internal/handler/dashboard_test.go:1510-1511`'in doğrusallaştırma sabitleriyle (`0.04045`, `12.92`); hesap betiği renkleri `tailwind.config.js`'den okur |
| Her `Page` ve panel kabuğu `Wordmark` ile açılır: `taptime` (tappa-green, 18 px kalın) + `punchless` (10 px mono, ink/70) | `web/templates/layout/base.templ:66-68`, `:124-129`; `web/templates/pages/admin.templ:387-388` |
| `punchless` tonu porcelain üstünde **5,70:1** | `base.templ:105-108` (ölçülmüş yorum, `TestBrand_EveryInkToneClearsAA` pinler) |
| Tap ekranı: docket (selamlama + mekân) + tek form + tek düğme | `web/templates/pages/tap.templ:47-65` |
| `.tap-button`: zemin tappa-green, metin paper, **20 px / 400**, en az 64 px | `web/static/css/input.css:192-196`; headless Chrome 154 computed style: `fontSize=20px fontWeight=400 minHeight=64px` |
| Tap düğmesi metni paper on tappa-green **7,73:1** | hesap (yukarıdaki betik) |
| Tap düğmesi doğrudan sayfa zemininde: `body` porcelain | `base.templ:66`; Chrome: `body bg=rgb(237, 240, 234)` |
| Tap ekranında başlık satırı **28 px**, düğmenin üst kenarı **214 px** (headless pencere 390'a inmedi; ölçüm 500×757'de — başlık yüksekliği genişlikten bağımsız) | gerçek `pages.Tap` şablonu, scratch modülde render edildi, Chrome `getBoundingClientRect` |
| `.tap-button` sınıfı **7** `class` özniteliğinde: tap ekranı **1** (`tap.templ:63`), aktivasyon/tur/problem/onay **6** (`activate.templ:177, 341, 344, 346, 425, 453`) | `rg -n 'tap-button' web/templates --glob '*.templ'` |
| Tap düğmesinin `:focus-visible` kuralı stil/kalınlık/ofset koyar, **renk koymaz**; Chrome 154'te odakta outline rengi **rgb(0, 95, 204)** (tarayıcının odak rengi), dinlenmede metin rengi (rgb(255, 253, 244)) | derlenmiş `app.css` (scratch derleme) `.tap-button:focus-visible{outline-offset:2px;outline-style:solid;outline-width:2px}`; Chrome computed style |
| `.btn`'in odak halkası ink | `input.css:203-207` |
| Beş kaşe damgası ve durum→renk eşlemesi | `input.css:180-189`; `TestCompiledCSS_StampWordIsInk` |
| Sonuç ekranında iki yeşil öğe var: `layout.Page` (`result.templ:33`) Wordmark'ı basar — yeşil `taptime` (`base.templ:68`, `:126`) — ve `.stamp--approved`. `result.templ`'in kendi `class`'larında tappa-green geçmez (tek isabet bir yorum) | `rg -n 'tappa-green' web/templates/pages/result.templ` → `:76` (yorum); `base.templ:126` |
| Wordmark satırı **28 px**; 10 px mono co-brand satırı tek başına **15 px** (satır yüksekliği 15 px) | headless Chrome 154, derlenmiş `app.css` ile `getBoundingClientRect` |
| Account bölümü panel kabuğunda render edilir ve bugün bir önizleme taşır: *"What your staff read"*, sonuç ekranının kendi `brandMessage` bileşeni | `web/templates/pages/account.templ:31` (`PanelShell`), `:415`, `:420`, `:424`; `TestAccount_TheBrandPreviewIsTheTapScreensOwnComponent` |
| `admin_users`'ta `UNIQUE (id, tenant_id)`; bileşik FK emsalleri var | `db/migrations/00006_create_admin_users.sql:85`, `:145`, `:196`; `00016_add_billing_price_and_periods.sql:572` |
| Sonuç ekranının bugünkü tek tenant'a bağlı öğesi: iş türüne göre seçilen marka cümlesi; tenant düzenleyemez | `result.templ:336-360`; `TestAccount_SaysTheMessagesCannotBeEdited` |
| `TapView` **3** alan, `ResultView` **8** alan | `web/templates/pages/view.go:167-181`, `:278-343` |
| Aktivasyon işveren adını üç yerde basar; GDPR cümlesi Taptime'ı **işleyen** olarak adlandırır: *"{EmployerName} is the controller of these records; Taptime processes them on their behalf"* | `web/templates/pages/activate.templ:37`, `:92-93`, `:174` |
| Aktivasyon yanıtları CSP başlığı **taşımaz** | `internal/handler/activate.go:1131-1135` (`render`: Content-Type, Cache-Control, nosniff); `rg -c 'Content-Security-Policy' internal/handler/activate.go` → 0 |
| Panel kabuğu: Wordmark + *"Signed in as"* + yöneticinin **kendi** adı + rol + çıkış + sekmeler; **tenant adı yok** | `admin.templ:387-405` |
| Panel kabuğunun kurucusu bir DB okuması yapar (bekleyen sayısı); hata sayfayı düşürmez, rozet `?` olur | `internal/handler/review.go:527-555` |
| Sayfa CSP'leri: `tapCSP` (`tap.go:471-472`; `Tap.render` ile tap, sonuç ve tap-problem yanıtlarına — `tap.go:635-646`) · `adminCSP` (`adminlogin.go:1680-1681`) · `adminScriptedCSP` (`:1752`) · `adminLoginCSP` (`:1796`) · `marketingCSP` (`marketing.go:452`) · `landingCSPFor` (`:500-509`) · `signupCSP` (`signup.go:946`). Yedisi de `style-src 'self'` taşır; `img-src` yalnız `landingCSPFor`'da, poster varken | `rg -n` ile sabitler okundu |
| Şablonlarda `<img` **0**, `style=` özniteliği **0** (tek isabet `landing.templ:52`'de bir yorum), `<style` **0**; üretim Go kodunda `'unsafe-inline'` veren politika **0** | `rg` sayımları |
| Yönetici çerezi `Path=/admin`, çalışan oturum çerezi `Path=/` | `internal/adminauth/cookie.go:48`; `internal/session/cookie.go:182` |
| `/static` düz `http.FileServer`; `/healthz` kimliksiz, DB'siz, iki baytlık yapısal rota | `internal/httpx/router.go:111`; `:99`, `:151-156` |
| Plakete yazılan NDEF URL'inin tabanı süreç yapılandırmasından gelir | `internal/encode/session.go:1127-1134`, `:1242` |
| *"taptime.mt altına güvenilmeyen bir alt alan adı eklenmez"* duran dağıtım kısıtı | `internal/handler/cookies.go:143-149` |
| Plaket-oturum tenant uyuşmazlığında tap sayfası mekân adı olmadan render edilir | `internal/handler/tap.go:387-395` (`ErrForeignLocation`) |
| Hesap yazma kapısı: canlı oturum + `owner` | `internal/handler/accountactions.go:95-97` (`mayEditAccount`) |
| Markaya ait kod/şema: `internal/brand` **yok**; `db/` altında `branding\|brand_` **0**; son migration `00026` | `ls`, `rg -i`, `ls db/migrations` |
| E-posta kodu yok: `internal/mail` **yok** (EM-2 başlamadı) | `ls` |
| `docs/adr` 0021'de biter; 0022 yazılmadı | `ls docs/adr` |

**OP-8 sonrası (`71272fa`) — 2026-10-02 notu.** Tablo `c0c0250`'yi sabitler. Dalın ucu (`m10-a1`
→ `99fa981`; OP-8 `71272fa`) `git show 71272fa:…` ile okunarak ölçüldü:
- `internal/httpx/router.go`'ya 7 satır eklendi: `:83` (`middleware.Timeout`) yerinde;
  `/healthz` `:99` → `:106`, `/static` `:111` → `:118`, `live` `:151-156` → `:158-163`.
- Operatör yüzeyi iki politika ekledi: `operatorCSP` (`internal/handler/operator/render.go:28-29`)
  ve `enrollCSP()` (`:39-41` — `operatorCSP` + tek bir script kaynağı). İkisi de `style-src 'self'`
  taşır, `img-src` taşımaz → dalın ucunda **dokuz** politika, dokuzu da `style-src 'self'`;
  `img-src` hâlâ yalnız `landingCSPFor`'da (`marketing.go:506`).
- `marketing.go`, `input.css` ve `base.templ`'de bu ADR'nin atıf yaptığı satırlar kaymadı
  (`marketing.go:391`, `:452`, `:473`, `:500-509`; `input.css:180-207`; `base.templ:66-68`,
  `:105`, `:124-126`).

## Karar

### 1. Girdiler: bir vurgu rengi ve bir logo

Tenant iki şey verir: **bir accent** (altı haneli hex) ve **bir logo** (ADR 0024). Başka marka
girdisi yoktur: yazı tipi, ikinci renk, metin, favicon kapsam dışıdır (bu ADR §9). Saklama: ayrı
`tenant_branding` tablosu, `tenants`'a 0..1 ilişki. Sütunlar: `tenant_id` birincil anahtar
(tablo-kısıtı biçiminde — R5b), `accent char(6)` CHECK `^[0-9A-F]{6}$`, logo alanları (ADR 0024
§4), `updated_at timestamptz`, `updated_by` ve **bileşik** `FOREIGN KEY (updated_by, tenant_id)
REFERENCES admin_users (id, tenant_id)` — güncelleyen yönetici aynı tenant'ın yöneticisi
olmak zorunda (§4.5 kuşağı; `admin_users_id_tenant_key` bunu taşır, Bağlam) (→ **WL-1**:
§6'nın beşlisi + GRANT SELECT/INSERT/UPDATE, DELETE yok;
`has_table_privilege('tappa_app','tenant_branding','DELETE') = false`; CHECK'ler hasmane
değerlerle patlatılır; başka tenant'ın yönetici id'siyle `updated_by` FK ile reddedilir; RLS
testi `WHERE`'siz A bağlamında B'yi 0 görür).

Marka hukuki delil değildir; değiştirilebilir (UPDATE), sıfırlama `UPDATE … NULL`'dır; geçmiş
`audit_log`'dadır (→ **WL-4**: kaydet/sil UPDATE + audit aynı transaction'da, `RecordTx`;
zorla patlatılan audit UPDATE'i geri alır, iki yönde).

**WL-1 notu (2026-10-03 — uygulama ve ölçüm; bu bölümün kuralı değişmedi).** Migration
`db/migrations/00028_create_tenant_branding.sql`, sorgular `db/queries/branding.sql`, testler
`internal/db/branding_test.go`. Ölçüm ortamı: dev Postgres 17, uygulama rolü `tappa_app`.
- **Bu bölümün ve ADR 0024 §4'ün yazmadığı, eklenen beş şey:** (1) `created_at timestamptz NOT
  NULL DEFAULT now()` (CLAUDE.md §6 tablo iskeleti); (2) `updated_by` **NOT NULL** — ADR 0024
  §6'da marka yazımı sahibin panel rotalarından gelir; (3) logo boyutuna alt sınır:
  `octet_length(logo) BETWEEN 1 AND 262144`; (4) `tenant_branding_logo_sha256_matches_logo`:
  `logo_sha256 = encode(sha256(logo), 'hex')` — rotalar `immutable` önbellekle sha adresli
  servis eder (ADR 0024 §5), kısıt adresin baytlara ait olmasını satırın özelliği yapar;
  (5) yetkiler sütun düzeyinde: `REVOKE ALL`, sonra `SELECT` (tablo) · `INSERT (tenant_id,
  updated_by)` · `UPDATE (accent, logo, logo_sha256, logo_mime, logo_width, logo_height,
  updated_at, updated_by)`; DELETE, TRUNCATE, REFERENCES, TRIGGER yok.
- **"tablo-kısıtı biçiminde — R5b" düzeltmesi:** bu biçimi isteyen R5b değil, R5'in indeks
  kuralıdır. Ölçüldü: aynı tablo `tenant_id uuid NOT NULL PRIMARY KEY` yazımıyla
  `scripts/redline-check.sh` → `[R5 · FAIL] … eksik → tenant_id ONDE olan indeks` (indeks
  katalogda aynı).
- **Benzersizlik (ADR 0024 WL-3 devri):** `logo_sha256` üzerinde UNIQUE yok; `(tenant_id,
  logo_sha256)` birincil anahtardan çıkar (tenant başına tek satır, tek sha).
- **`accent char(6)`:** açık `::char(6)` dönüşümü uzun değeri sessizce keser
  (`'1F5C41ZZ'::char(6)` = `1F5C41`, CHECK'ten geçer — ölçüldü); sorgular değeri `text` olarak
  verir. Atamada fazla karakterlerden biri boşluk değilse 22001; fazla karakterlerin hepsi
  sondaki boşluksa kırpılır ve değer kanonik saklanır (`1F5C41   ` → `1F5C41`, kabul —
  ölçüldü, `TestTenantBranding_ChecksRefuseHostileValues` içinde ayrı vaka). Saklanan değer
  argümanın metninden farklı olabilir; WL-4 audit "after"unu DB'den okur ya da `Color.Hex()`
  kullanır. Mutant `sqlc.arg(accent)::char(6)`: `1F5C41A` kesilip kabul edildi.
- **Sorgular** (planın `UpsertTenant*` adlarının yerine): `GetTenantBrand` ve
  `GetTenantBrandForUpdate` (`logo` seçmez) · `GetTenantLogo` (tenant + sha) ·
  `EnsureTenantBrand` · `SetTenantAccent` · `ClearTenantAccent` · `SetTenantLogo` ·
  `ClearTenantLogo`. WL-4'ün yazma sırası Ensure → ForUpdate → Set/Clear → audit'tir; ölçülen
  iki sırada (yeni satır: ikinci yazıcı Ensure'da bekler; var olan satır: ForUpdate'te bekler)
  ikinci yazıcının "önceki" değeri birincinin yazdığıdır. Ensure atlanırsa, birincinin
  commit edilmemiş ilk satırı varken kilit satır bulmaz (`pgx.ErrNoRows`, ölçüldü).
- **Güvenlik iddiası (WL-1, üç parçalı).**
  - **PART I — ölçülen davranış:** A bağlamında `WHERE`'siz `SELECT tenant_id FROM
    tenant_branding` yalnız A'yı döndürür, B'nin satırı B bağlamında okunur; A bağlamında
    `GetTenantLogo` B'nin sha'sıyla, filtre A'yı da B'yi de adlandırsa `pgx.ErrNoRows` alır
    (`TestRLS_TenantBranding_ReadIsolationWithoutWhere`) · A bağlamında B'nin (satırı var) ve
    C'nin (satırı yok) `tenant_id`'siyle INSERT 42501 RLS ile reddedilir, iki ret aynı kod ve
    mesajdır; `WHERE`'siz UPDATE yalnız A'nın satırını değiştirir; `tenant_id`'yi değiştiren
    UPDATE yetkiyle reddedilir (`TestRLS_TenantBranding_WriteWithCheck`) · bir kez yazılıp
    boşalmış GUC'lu bağlantıda `WHERE`'siz okuma hata vermeden 0 satır döner
    (`TestRLS_TenantBranding_NoContextFailsClosed`) · `has_table_privilege('tappa_app',
    'tenant_branding','DELETE') = false`, sütun yetki matrisi yukarıdaki gibi, `DELETE` yetkiyle
    reddedilir (`TestTenantBranding_AppPrivileges`) · 34 hasmane değer adı verilen kısıta ya da
    22001'e takılır, sonu boşluklu accent kabul edilip kanonik saklanır
    (`TestTenantBranding_ChecksRefuseHostileValues`) · başka tenant'ın yönetici
    id'si `updated_by`'da 23503 `tenant_branding_updated_by_fk` alır
    (`TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant`) · aynı baytlar iki tenant'ta
    kabul edilir (`TestTenantBranding_SameLogoInTwoTenants`) · `GetTenantBrand` ve
    `GetTenantBrandForUpdate`'in sevk edilen metninde seçim listesi tam olarak yedi sütundur,
    `logo` belirteci ve `*` yoktur (`TestTenantBranding_PerPageReadsDoNotSelectTheLogo`) · yedi
    SELECT/UPDATE'in `WHERE`'i `tenant_id = $n` taşır
    (`TestTenantBranding_EveryStatementNamesTheTenant`).
  - **PART II — yapıcının koşturduğu mutasyonlar ve kırmızıya dönen pinler (bu liste o
    koşuların tamamı; her mutasyon tek düzenleme).** Migration mutasyonları dev'e Up edildi,
    test koşuldu, Down edildi; ilk koşuları test dosyasının o günkü sürümüneydi ve 2. turda
    üçüncü göz hepsini son test sürümüne karşı yeniden üretti — aynı pinler kırmızıya döndü.
    Sorgu mutasyonları (aşağıda ayrı) DDL'siz: `db/queries/branding.sql` değişti, `make sqlc`,
    test koşuldu, geri alındı.
    *Migration:* FORCE kaldırıldı → `TestTenantBranding_CatalogShape`,
    `TestRLS_EveryTenantScopedTableIsEnabledAndForced`, redline R5 ·
    politikadan `NULLIF` kaldırıldı → `TestRLS_TenantBranding_NoContextFailsClosed`,
    `TestTenantBranding_CatalogShape` (redline R5 geçti) ·
    `WITH CHECK` kaldırıldı → `TestTenantBranding_CatalogShape`, redline R5 ·
    DELETE grant'ı → `TestTenantBranding_AppPrivileges` ·
    tablo düzeyi INSERT → `TestTenantBranding_AppPrivileges` ·
    `REVOKE ALL` kaldırıldı (dev'in `arwd` varsayılanında) → `TestTenantBranding_AppPrivileges`,
    `TestRLS_TenantBranding_WriteWithCheck`; dar `ar` varsayılanında (üçüncü göz, 2. tur) →
    `TestTenantBranding_AppPrivileges` (tablo düzeyi INSERT, on INSERT sütunu) ·
    accent CHECK'i küçük harfe açıldı · boyut sınırı 262145 · hep-ya-hiç CHECK'i kaldırıldı ·
    `_matches_logo` kaldırıldı · `_hex` kaldırıldı · mime kümesine `image/gif` →
    `TestTenantBranding_ChecksRefuseHostileValues` ·
    FK tek sütunlu → `TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant`,
    `TestTenantBranding_CatalogShape` ·
    `logo_sha256` üzerinde global UNIQUE → `TestTenantBranding_SameLogoInTwoTenants`,
    `TestTenantBranding_CatalogShape` ·
    birincil anahtar kaldırıldı → `internal/db/branding_test.go`'daki DB testlerinin onu
    (`ON CONFLICT` 42P10 ve katalog), redline R5 ·
    birincil anahtar sütun yazımında → redline R5 (davranış ve katalog testleri yeşil).
    *Sorgu:* `SetTenantAccent`'te `sqlc.arg(accent)::char(6)` → `TestTenantBranding_ChecksRefuseHostileValues` ·
    `GetTenantBrand`'e `logo` → `TestTenantBranding_PerPageReadsDoNotSelectTheLogo`,
    `TestStoreSurface_IsTheOneRecorded`, `TestStoreSurface_NoByteCarryingQueryReadsTags` ·
    `GetTenantBrand`'in seçiminde `to_jsonb(tenant_branding)::text AS accent` → sqlc alan tipini
    değiştirdi, `internal/db` test paketi derlenmedi; `TestStoreSurface_IsTheOneRecorded`
    (metin pininin bu metin üzerindeki kararı gözlenmedi) ·
    `GetTenantLogo`'nun `WHERE`'inde `tenant_id = @tenant_id` yerine `@tenant_id::uuid IS NOT
    NULL` → `TestTenantBranding_EveryStatementNamesTheTenant` (bu test eklenmeden önce
    `internal/db/branding_test.go`'daki testler ve `TestStoreSurface_*` yeşildi) ·
    `GetTenantBrandForUpdate`'ten `FOR UPDATE` kaldırıldı →
    `TestTenantBranding_EnsureThenLockReturnsWhatTheOtherWriterCommitted`.
    Yeşil kalanlar ve sebepleri: `WITH CHECK`'in kaldırılmasında davranış testleri —
    `USING`'i olup `WITH CHECK`'i olmayan bir ALL politikası yazmada da `USING`'i uygular
    (PostgreSQL kuralı; testlerin yeşil kalmasıyla tutarlı); FORCE'un kaldırılmasında davranış
    testleri — FORCE tablo sahibini bağlar, `tappa_app` sahip değildir; `GetTenantLogo`'nun
    `WHERE`'i `(tenant_id = @tenant_id OR true)` olunca bütün testler (üçüncü göz, 2. tur) — belt
    testinin deseni yüklemin varlığına bakar, anlamına bakmaz; RLS satırı yine gizler. Down'ın
    boşaltılması bir testle değil ölçümle görüldü: boş Down sonrası şema dökümü v28'inkine eşit
    kaldı ve sonraki Up 42P07 ile düştü. Kontrol koşusu: iki sha CHECK'inin bildirim sırası
    değişti → yeşil (büyük harf sha yine `_hex` ile reddedildi; değerlendirme ad sırasıyla).
  - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**WL-4 notu (2026-10-03 — uygulama ve ölçüm; bu bölümün kuralı değişmedi).** Kod
`internal/domain/tenant/brand.go` (`Brands`: `SaveAccent`, `ClearAccent`, `SaveLogo`,
`ClearLogo`), testler `internal/domain/tenant/brand_db_test.go`; ek olarak
`internal/brand/logo.go`'ya `Logo.Normalized` (aşağıda karar 5 — WL-3'ün dosyası, kapsam
genişlemesi) ve `internal/brand/logo_normalized_test.go`. Ölçüm ortamı: dev Postgres 17,
`tappa_app`; yerel Go 1.27.1. Yeni sorgu, migration ve bağımlılık yok.
- **Karar 1 — `detail`'in altı anahtarı ADR 0024 §6'nınkidir** (`field`, `before`, `after`,
  `bytes`, `width`, `height`); bu not yalnız değerlerini yazar. `field`: `accent` ya da `logo`.
  `before`/`after`: accent'in altı hanesi ya da logonun sha256'sı, yoksa `null`; "before"
  `GetTenantBrandForUpdate`'ten, "after" Set/Clear'dan sonra aynı transaction'da okunan
  `GetTenantBrand`'den. `bytes`: logo kaydında yazılan baytın sayısı (geri okunan sha yazılanla
  eşit değilse yazma hata verir — **kodda, ölçülmedi**: birincil anahtar ve
  `_matches_logo` CHECK'i varken testler bu dala ulaşmaz, dal kaldırılınca hepsi yeşil kaldı,
  mutasyon A15), diğer üç işlemde `null`. `width`/`height`: "after"ta logo
  varsa DB'nin değeri, yoksa `null`. `omitempty` yok — `null` anahtarı silmez. Eylem
  `tenant.brand_updated`, `actor_id` = `ActorID`, `target` = tenant id (`Accounts.Save`
  emsali). Logonun baytı, dosya adı ve istemci `Content-Type`'ı `detail`'i dolduran kodda
  geçmez (ADR 0024 İddia G).
- **Karar 2 — yetki: aktif owner, domain'de ikinci kez.** ADR 0024 §6 owner'ı adlandırır ve
  kapıyı handler'a koyar (`mayEditAccount`, WL-7); domain onu tekrar sorar: yazmanın kendi
  transaction'ında, tenant bağlamında `GetAdminByID` ile `role = 'owner'` ve
  `status = 'active'`. Manager, devre dışı owner, başka tenant'ın yöneticisi ve yönetici
  olmayan id aynı `ErrBrandNotPermitted` değerini (sarılmadan, tek metinle) alır; hangisi
  olduğu söylenmez; yazma, satır yaratma ve audit 0. Kapı `EnsureTenantBrand`'den önce koşar:
  sonrasına taşınırsa (mutasyon A01) satırı olmayan tenant'ta o tenant'ın yöneticisi olmayan
  üç id 23503 FK hatası, manager ve devre dışı owner `ErrBrandNotPermitted` alır — yanıt
  hangisi olduğunu söyler.
  Gerekçe: domain accent'i handler'a güvenmeden yeniden `Check` ettiği gibi yetkiyi de
  yeniden sorar; rol oturumdaki iddiadan değil DB'den okunur. Ret satırı
  (`tenant.brand_update_refused`) domain'in değil WL-7'nin — `accountactions.go`'nun emsali.
  WL-7 domain'in `ErrBrandNotPermitted`'ini de kendi reddi gibi ele alır: yalnız 303
  `not-permitted` değil, **`tenant.brand_update_refused` satırını da yazar** — ana
  transaction geri alındığı için `RecordTx` ile değil, kendi transaction'ını açan
  `audit.Recorder.Record` ile (ADR 0024 §6; `internal/audit/audit.go`). Policy motoru
  seçilmedi: marka için bir eylem ve guardrail ADR 0023/0024'te yok; eklemek §5/§3 değişikliği
  olurdu.
- **Karar 3 — aynı değeri yeniden kaydetmek bir eylemdir:** UPDATE koşar (`updated_at`,
  `updated_by` değişir) ve "before" = "after" olan bir satır yazılır (`Accounts.Save`
  emsali; `TestBrandDB_SavingTheSameAccentAgainIsRecorded`). Var olmayan satırı temizlemek
  de satırı yaratır (`EnsureTenantBrand`) ve `before`, `after`, `bytes`, `width`, `height`
  `null` olan bir satır yazar (`TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt`).
- **Karar 4 — accent girdisi `brand.Color`'dır** (WL-7'de `NormalizeAccent`'in çıktısı);
  domain `Check` eder, okunaksızsa `brand.ErrAccentIllegible` transaction açılmadan döner;
  saklanan `Color.Hex()`'tir.
- **Karar 5 — logo girdisi `brand.Logo`'dur; `Normalized()` yanlışsa `SaveLogo`
  transaction açmadan `ErrBrandLogoNotNormalized` döner.**
  `brand.Logo`'nun alanları dışa açıktır; kapı olmasaydı tablo ayırt edemezdi — ölçüldü:
  domain kontrolü kaldırılınca (mutasyon M12a) APP1 `Exif` segmentli bir JPEG, kendi sha'sıyla
  bir `Logo` literaline konup **saklandı**. `Normalized()` `Normalize`'ın döndürdüğü değere
  dışa kapalı bir `mint` iliştirir ve beş alanın hâlâ o değer olduğunu (baytın sha256'sı
  dahil) sınar; paket dışındaki bir literal `mint` taşımaz. Ölçüm:
  `TestLogo_NormalizedIsTrueOnlyForNormalizeOutput` (PNG ve JPEG çıktısı, kopyası, beş
  alanın her biri değişince, yerinde değişen bir bayt, aynı beş değerli literal, sıfır
  `Logo`); `logoCheckOutput`'a verilen logolarda `Normalized()` doğru.
- **Ölçüm — iki yazıcı nerede bekler** (`TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore`,
  ikinci yazıcının `pg_stat_activity`'deki ifadesi): yeni satırda `EnsureTenantBrand`
  (birincinin commit edilmemiş INSERT'i); var olan satırda, birinci UPDATE'inden **önce**
  tutulunca `GetTenantBrandForUpdate`, **sonra** tutulunca `EnsureTenantBrand` (birincinin
  commit edilmemiş yeni satır sürümü). WL-1'in "var olan satırda ForUpdate'te bekler" ölçümü
  birincinin UPDATE'ten önceki hâlidir. Dört durumda ikincinin "before"u birincinin
  "after"ıdır.
- **Güvenlik iddiası (WL-4, üç parçalı).**
  - **PART I — ölçülen davranış** (dev Postgres, `tappa_app`, testlerin adlandırdığı
    girdilerde): SaveAccent → SaveLogo (PNG) → SaveLogo (JPEG) → ClearAccent → ClearLogo dizisi
    her değeri saklar, `updated_by`'ı yazan owner yapar, yazma başına bir
    `tenant.brand_updated` satırı ekler; ClearLogo'dan sonra beş logo sütunu tek tek NULL
    (`TestBrandDB_EachWriteStoresItsValueAndOneTrailRow`) · dört yazmanın eklediği satırın
    anahtarları tam olarak altıdır (`TestBrandDB_TheDetailHasExactlyTheSixKeys`) · hata
    döndüren trail ile dört yazma, satırı olmayan ve olan tenant'ta satırı olduğu gibi bırakır
    (satır yaratmaz); satırını yazıp sonra hata döndüren trail ile ne değişiklik ne o satır
    kalır; kendisine verilen transaction'dan marka satırını okuyan trail her yazmanın kendi
    sonucunu görür (`TestBrandDB_TheChangeAndItsTrailRowShareOneTransaction`) · Set/Clear
    ifadesi hata döndürünce ya da 0 satır değiştirince dört yazma satırı ve trail'i olduğu gibi
    bırakır (`TestBrandDB_AFailedWriteLeavesNoTrailRow`) · iki okunaksız accent, değişmemiş
    `Normalize` çıktısı olmayan dört logo değeri, nil aktör ve nil tenant (dört yazmada)
    transaction açılmadan reddedilir, satır ve trail olduğu gibi kalır
    (`TestBrandDB_RefusalsBeforeTheDatabaseWriteNothing`) · manager, devre dışı owner, öbür
    tenant'ın owner'ı bu tenant'ta, bu tenant'ın owner'ı öbür tenant'ta ve yönetici olmayan id
    dört yazmada, marka satırı olan iki tenant'ta ve olmayan iki tenant'ta
    `ErrBrandNotPermitted` değerinin kendisini (`==`) beşi için tek metinle alır; iki
    tenant'ın satırı ve trail'i değişmez, satır yaratılmaz
    (`TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite`) · satırı olmayan tenant'ta
    ClearAccent ve ClearLogo satırı bütün marka alanları NULL olarak yaratır ve `before`,
    `after`, `bytes`, `width`, `height` `null` olan bir satır yazar
    (`TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt`) · eylem ve iki `field`
    değeri ADR 0024 §6'nın yazımıdır, testler trail'i literal eylem adıyla okur
    (`TestBrandTrail_TheActionAndFieldNamesAreTheADRs`) · iki eşzamanlı SaveAccent'in dört
    durumunda ikinci yukarıdaki ifadede bekler ve "before"u birincinin "after"ıdır
    (`TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore`) · saklı accent'i
    yeniden kaydetmek UPDATE'i koşar ve "before" = "after" satırı yazar
    (`TestBrandDB_SavingTheSameAccentAgainIsRecorded`) · `Normalized()` yukarıdaki karar 5'in
    girdilerinde (`TestLogo_NormalizedIsTrueOnlyForNormalizeOutput`).
  - **PART II — koşturulan mutasyonların tamamı ve kırmızıya dönen testleri** (her biri tek
    düzenleme; kopyala → düzenle → koş → geri yaz, yedekler scratchpad'de; sorgu mutasyonları
    `make sqlc` ile, DDL yok; koşulan testler: `internal/domain/tenant` içinde
    `TestBrandDB_*`, `TestNewBrands_RefusesAMissingDependency`, 2. turda ayrıca
    `TestBrandTrail_TheActionAndFieldNamesAreTheADRs`,
    `TestStaffQueries_CarryAnExplicitTenantPredicate`; `internal/brand` içinde
    `TestLogo_NormalizedIsTrueOnlyForNormalizeOutput`, `TestLogoMetadata_*`, `TestLogoOutput_*`;
    `internal/db` içinde `TestTenantBranding_*`, `TestRLS_TenantBranding_*`). Kısaltmalar:
    *Each* = `TestBrandDB_EachWriteStoresItsValueAndOneTrailRow`, *Keys* =
    `TestBrandDB_TheDetailHasExactlyTheSixKeys`, *Tx* =
    `TestBrandDB_TheChangeAndItsTrailRowShareOneTransaction`, *Fail* =
    `TestBrandDB_AFailedWriteLeavesNoTrailRow`, *Refuse* =
    `TestBrandDB_RefusalsBeforeTheDatabaseWriteNothing`, *Owner* =
    `TestBrandDB_OnlyAnActiveOwnerOfThisTenantMayWrite`, *Conc* =
    `TestBrandDB_TheSecondOfTwoConcurrentSavesRecordsTheFirstAsItsBefore`, *Same* =
    `TestBrandDB_SavingTheSameAccentAgainIsRecorded`.

    | # | Mutasyon | Kırmızı |
    |---|---|---|
    | M01a | audit, Set'ten sonra **iç içe ayrı** bir `WithTenant` transaction'ında | *Tx* (trail'in transaction'ı eski değeri gördü), *Refuse* (pozitif kontrolde 2 yerine 4 transaction) |
    | M01b | audit, değişiklik commit edildikten **sonra** ayrı transaction'da | *Tx* (hata döndüren ve yazıp-hata-döndüren trail: değişiklik kaldı), *Refuse* (pozitif kontrol), *Conc* (birinci UPDATE'ten sonra tutulan iki durum) |
    | M02 | "before" `FOR UPDATE`'siz `GetTenantBrand`'den | *Conc* (var olan satır, birinci UPDATE'ten önce tutulu: ikinci beklemedi) |
    | M03a | `SetTenantAccent`'e `updated_by = uuid.Nil` | *Each*, *Keys*, *Tx*, *Fail*, *Refuse*, *Owner*, *Conc*, *Same* (23503 `tenant_branding_updated_by_fk`) |
    | M03b | sorgu: `updated_by = COALESCE(updated_by, @updated_by)` (eski yazar kalır) | *Same*, *Conc*, `TestTenantBranding_UpdatedByIsAnAdminOfTheSameTenant` |
    | M04a | `SaveAccent`'ten `requireActor` | *Refuse* (nil aktör ve nil tenant: transaction açıldı, hata başka) |
    | M04b | `ClearLogo`'dan `requireActor` | *Refuse* (aynı iki satır, ClearLogo) |
    | M05 | domain `brand.Check`'i | *Refuse* (`808080`, `E0457B` yazıldı) |
    | M06a | `Hex()` yerine renk girdisinin yazımı `#rrggbb` | *Each*, *Keys*, *Tx*, *Fail*, *Refuse*, *Owner*, *Conc*, *Same* (22001) |
    | M06b | `Hex()` yerine küçük harf `rrggbb` | aynı sekiz test (23514 `tenant_branding_accent_canonical`) |
    | M06c | kontrol: accent "after"ı DB'den değil argümandan | **yeşil — eşdeğer**: argüman `Hex()`'tir, altı büyük harf hane, boşluk yok; WL-1'in kırpma vakası bu girdiyle oluşmaz |
    | M07 | `detail`'e yedinci anahtar `file_name` | *Each*, *Keys*, *Conc*, *Same* |
    | M07b | `height`'a `omitempty` (eksik anahtar) | *Each*, *Keys* (SaveAccent, ClearAccent, ClearLogo), *Conc*, *Same* |
    | M08 | "before" (`ForUpdate`, ErrNoRows → boş) Ensure'dan **önce** | *Conc* (yeni satırın iki durumu: "before" `null`; var olan satır UPDATE'ten sonra: ikinci `GetTenantBrandForUpdate`'te bekledi) |
    | M09 | Set/Clear'ın 1 satır beklentisi | *Fail* (0 satır durumu) |
    | M10a | sorgu: `ClearTenantLogo`'dan `logo_width = NULL` | *Each*, *Keys*, *Tx*, *Owner* (pozitif kontrol), `TestTenantBranding_StoreRoundTrip` (23514 `tenant_branding_logo_all_or_none`) |
    | M10b | `ClearLogo` `ClearTenantAccent`'i çağırır | *Each*, *Tx* |
    | M11 | sorgu: `SetTenantAccent` `WHERE (tenant_id = @tenant_id OR true)` | `TestStaffQueries_CarryAnExplicitTenantPredicate`, `TestRLS_TenantBranding_WriteWithCheck`; davranış testleri yeşil (RLS satırı gizler) |
    | M12a | `SaveLogo`'dan `Normalized()` kontrolü | *Refuse* (literal, Exif'li ham JPEG ve alanı değişmiş çıktı **yazıldı**; sıfır `Logo` transaction açtı) |
    | M12b | `Normalized()` hep `true` | *Refuse*, `TestLogo_NormalizedIsTrueOnlyForNormalizeOutput` |
    | M12c | `Normalized()` yalnız `mint`'in varlığına bakar | *Refuse* (alanı değişmiş çıktı), `TestLogo_NormalizedIsTrueOnlyForNormalizeOutput` |
    | M12d | `Normalized()`'dan `SHA256` dizgesi karşılaştırması | `TestLogo_NormalizedIsTrueOnlyForNormalizeOutput` |
    | M13a | domain rol kapısı (`mayEditBrand` çağrısı) | *Owner* (manager ve devre dışı owner **yazdı**; başka tenant'ın ve yönetici olmayan id 23503 FK ile reddedildi, satırlar değişmedi — hata `ErrBrandNotPermitted` değil) |
    | M13b | rol koşulu (manager kabul) | *Owner* (manager satırları) |
    | M13c | durum koşulu (devre dışı kabul) | *Owner* (devre dışı owner satırları) |
    | M14 | trail hatası yutulur | *Tx* (iki hata durumu) |
    | M15 | `EnsureTenantBrand` çağrısı | *Each*, *Keys*, *Tx*, *Fail*, *Refuse*, *Owner*, *Conc*, *Same* |
    | M16 | "after" = "before" (geri okuma yok) | *Each*, *Keys*, *Tx*, *Fail*, *Refuse*, *Owner*, *Conc* |
    | M17 | Set/Clear'ın hatası yutulur | *Fail* ("ifade hata verir": yalnız hata metni — 1 satır beklentisi 0 satırı yakaladı, satır ve trail değişmedi) |
    | A01 (2. tur) | rol kapısı `EnsureTenantBrand`'den **sonra** | *Owner* (satırsız tenant turu: başka tenant'ın owner'ı bu tenant'ta, bu tenant'ın owner'ı öbür tenant'ta ve yönetici olmayan id dört yazmada 23503 aldı; manager ve devre dışı owner `ErrBrandNotPermitted`; üç farklı hata metni. Satırlı tur yeşil kaldı) |
    | A07c (2. tur) | rol ve durum hataya sarılır (`%w: role …, status …`) ve `write()`'taki `ErrBrandNotPermitted` toparlaması kaldırılır | *Owner* (iki tur: değer `==` değil, altı farklı metin) |
    | A12 (2. tur) | `ActionBrandUpdated` = `tenant.brand_changed` | `TestBrandTrail_TheActionAndFieldNamesAreTheADRs`, *Each*, *Keys*, *Conc*, *Same*, *Clear* |
    | A15 (2. tur) | `brandDetailOf`'un geri-okuma sha kontrolü | **yeşil** — dal ulaşılamaz (Karar 1'de "kodda, ölçülmedi") |

    *Clear* = `TestBrandDB_ClearingWithoutABrandRowCreatesTheRowAndRecordsIt`. Pozitif kontrol:
    değiştirilmemiş kodda aynı test kümesi yeşil (M00; 2. turun testleriyle yeniden). M01a, M01b
    ve M02 test dosyasının 1. tur son sürümüne karşı yeniden koşuldu (eşzamanlılık testi M02'den
    sonra yeniden yazıldı: ilk sürümü yalnız "UPDATE'ten sonra" tutuyordu ve M02 yeşil kalmıştı —
    o tutmada ikinci yazıcı `EnsureTenantBrand`'de bekler, `FOR UPDATE`'e ulaşmaz). M-satırları
    2. turun eklediği testlere karşı yeniden koşulmadı; A-satırları 2. turun test dosyasına karşı
    koşuldu.
  - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
- **Sayılı sınırlar.** (1) Rol okuması düz bir `SELECT`'tir: okumadan sonra commit edilen bir
  rol düşürmesini o yazma görmez (ölçülmedi). (2) `Normalized()` `reflect`/`unsafe` ile
  aşılabilir; `Normalize` çıktısının değişmemiş kopyası kabul edilir (tasarım). (3) Audit
  satırlarının sırası sınanmaz (`at` = transaction başlangıcı). (4) Test fikstürleri dev DB'de
  kalır (`tappa_app` DELETE taşımaz; `make db-reset`). (5) Eşzamanlılık testi
  `pg_stat_activity`'yi `tappa_app` olarak okur (aynı rolün oturumu görünür).
  (6) `log_statement=all` olan dev'de testlerin üretilmiş logo baytları sunucu log'una
  parametre olarak düşebilir (sır değil; WL-1 sınırıyla aynı — log okunmadı).

### 2. Slot haritası — ekran ekran

Tablo **sayılı listedir**: bir yüzey burada yoksa marka almaz, ve eklenmesi bu ADR'nin
değişikliğidir.

| Yüzey | Logo | Accent | Tenant adı | Taptime izi | Bağlı görev / kabul |
|---|---|---|---|---|---|
| **Tap ekranı** (`GET /t`) | başlıkta, Wordmark'ın yerinde; **altında** co-brand satırı (K-2b) | **tap düğmesinin zemini** (+ OnColor metin, gerektiğinde Edge — §3) | yalnız logonun `alt`'ı | logo varken co-brand satırı (K-2b); logo yokken Wordmark, `taptime` accent varsa **ink** (K-2a), yoksa bugünkü gibi | **WL-9** — D-C, K-2a ve K-2b kartta alıntılı; hâlâ tek düğme, metni *"Tap"*, ≥64 px; 390×844'te düğmenin üst kenarı ≤16 px kayar (§5'in aritmetiği); logo `width`/`height` → layout-shift 0 |
| **Sonuç ekranı** (`POST /api/checkin` → `pages.Result`) | başlıkta; altında co-brand satırı (K-2b) | **yok** | yalnız `alt` | logo varken co-brand satırı; logo yokken bugünkü Wordmark (accent olmadığı için K-2a burada uygulanmaz) | **WL-9** — `ResultView` 8 alan kalır (marka ayrı açık parametre); markalı tenant'ın sonuç sayfasında tema `<link>`'i **0** |
| **Panel kabuğu** (`PanelShell`, `PanelShellWithScript`) | başlıkta | **4 px dekoratif şerit** (K4) | görünür metin | co-brand satırı | **WL-8** — `chrome()` +1 PK okuması; marka okuma hatası sayfayı düşürmez; OP-10'dan sonra |
| **Panel → Account → *"Your brand"* önizlemesi** (panel kabuğunun içinde) | önizlemedeki tap ekranının başlığında, kaynağı `/admin/brand/logo/{sha}` | önizlemedeki **tap düğmesinin zemini** — **kaydedilmiş** accent | önizlemede `alt` | önizleme tap ekranının kendisidir | **WL-7** (WL-9'a bağımlı) — aşağıdaki *"Account önizlemesi"* paragrafının kuralları |
| Tur / practice / *"You're set up"* (`/activate/tour`, `/activate/done`) | faz 1: yok | yok | — | Taptime Wordmark | faz 2 — *Karar verilmedi* |
| Aktivasyon (`/activate`, onay ekranı) | yok (K6) | yok | bugünkü gibi metin (işveren adı) | Taptime Wordmark | değişmez |
| Problem sayfaları (tap ve aktivasyon akışları) | yok | yok | — | Taptime | değişmez |
| AdminChoose, yönetici girişi, parola sıfırlama | yok | yok | — | Taptime | değişmez (WL-8 kabulü: AdminChoose değişmez) |
| Landing, legal, signup | yok | yok | — | Taptime | değişmez |
| Operatör yüzeyi (`/operator`) | yok | yok | OP-8'in başlık kuralı | *TAPTIME OPERATOR* kabuğu | değişmez; bu ADR `op_*` eklemez |
| E-posta | faz 1: yok (K8) | yok | gövdede, kaçışlı | gönderen sabit Taptime (EM-K5) | **WL-11** (SES akışıyla) |
| CSV / rapor | yok | yok | — | değişmez | — |

**Uyuşmazlık kuralı.** Plaket başka bir tenant'ınsa (`ErrForeignLocation`, `tap.go:387-395`)
tap ve sonuç ekranı **Taptime varsayılanıyla** render edilir: logo yok, düğme varsayılan
renkte. Gerekçe: iki tenant'tan birinin markası iki durumda da yanlış olurdu (→ **WL-9**: A
çalışanı B plaketinde, tap **ve** sonuç sayfasının gövdesinde `/t/logo/` 0 isabet ve tema
`<link>`'i yok).

**Account önizlemesi — kurallar** (→ **WL-7**; bölme → **WL-9**):
- **Gönderilemez:** önizlemenin HTML'inde `<form` 0 ve `/api/checkin` 0; önizleme düğmesi
  `type="submit"` değildir (`type="button"` ya da düğme olmayan bir öğe), hiçbir `<form>`'un
  soyundan değildir ve `form=` özniteliği taşımaz. Gerekçe: tap ekranının düğmesi
  `<button type="submit">`'tir (`tap.templ:63`); editörün kendi `<form>`'unun içine düşen bir
  `submit` düğmesi editörü gönderir.
- **Logo kaynağı:** `/admin/brand/logo/{sha}` — yönetici çerezi `Path=/admin`; `/t/logo/…`
  canlı çalışan oturumu ister ve panelde kullanılamaz.
- **Accent: yalnız kaydedilmiş olan.** Önizleme panel kabuğunun tek tema `<link>`'ini kullanır;
  formdaki aday hex önizlemeye taşınmaz. Gerekçe: §4'ün tek enjeksiyon yolu `:root` tema
  dosyasıdır; aday için ikinci bir `<link>` `:root`'u yeniden yazar ve K4 şeridini de boyar;
  önizlemeye kapsamlı bir değişken ise CSP `style-src 'self'` altında satır içi stil olmadan
  yeni bir rota ister. Aday renk, editörün `<input type="color">`'unun tarayıcının kendi çizdiği
  rengiyle görünür (CSS gerektirmez); okunaksız adayda form önerilen hex'i gösterir (WL-7).
- **"Gerçek bileşenler" ve bölme:** `templ Tap` docket'ı ve formun içindeki düğmeyi tek
  bileşende satır içi yazıyor (`tap.templ:49-57`, `:58-64`). Önizlemenin aynı bileşenleri formsuz
  render edebilmesi için bunların ayrılması gerekir. **Bölmeyi WL-9 yapar**, WL-7 WL-9'a
  bağımlıdır. Gerekçe: `tap.templ`'in sahibi zaten WL-9'dur ve WL-9'un golden testi bölmeden
  sonra markasız tap ekranının HTML + CSP'sinin bayt-aynı kaldığını gösterir — tap ekranının
  davranışı değişmez (§9). (→ **WL-9** kabulüne: bölme sonrası tap ekranı golden'ı bayt-aynı;
  önizleme ile tap ekranı aynı başlık ve düğme yüzü bileşenlerini çağırır.)

**`.tap-button` yedi yerde; accent'i iki sayfada alır.** Sınıf aktivasyon, tur, problem ve
onay ekranlarında da kullanılıyor (ölçüldü, 6 öznitelik). Accent'i sınıf değil **sayfanın tema
bağlantısı** taşır (§4): tema `<link>`'i tap ekranına ve panel kabuğuna eklenir. Panel
kabuğunun içinde render edilen Account önizlemesi tap ekranının düğmesini taşıdığı için o düğme
accent'i **bilerek** alır (önizleme tap ekranını gösterir). Diğer altı kullanımın sayfası temayı
yüklemez ve `:root` varsayılanı tappa-green'dir (→ **WL-5**: marka ayarlamamış tenant'ta
düğmenin computed rengi `rgb(31,92,65)` / `rgb(255,253,244)`; **WL-9**: marka yoksa HTML +
CSP bayt-aynı, golden).

### 3. Accent — WCAG kapısı (K1)

**Tanımlar** (WCAG 2.x, sRGB): kanal `c = v/255`, `c ≤ 0.04045 ? c/12.92 :
((c+0.055)/1.055)^2.4`; `L = 0.2126 R + 0.7152 G + 0.0722 B`; kontrast `(L₁+0.05)/(L₂+0.05)`,
`L₁ ≥ L₂`. **Palet sabitleri Go'da bir kopya olarak durur** — `internal/brand` dosyayı
gömemez (`go:embed` paket dizininin dışına, `..` ile çıkamaz) — ve kopya bir testle
`tailwind.config.js`'e bağlanır (→ **WL-2**: test `tailwind.config.js`'i okur, kopyadaki dokuz
token'ın hex'iyle eşitliğini sınar; bir renk değişirse kırmızı).

**Accent bir dolgudur, metin rengi değildir.** Kapı accent'in **üstündeki** metni (paper ya
da ink) ve accent'in porcelain'e karşı sınırını ölçer; accent'in **kendisinin** bir zeminde
metin ya da ince çizgi olarak okunurluğunu ölçmez (ör. `#FFC72C` porcelain'de 1,36:1). Bu
yüzden accent §2'deki dolgularla sınırlıdır: tap düğmesinin zemini (tap ekranında ve Account
önizlemesinde) ve panel şeridi. **Özellik kuralı** (→ **WL-5**): derlenmiş CSS'te
`--brand-accent` yalnız `background-color` bildirimlerinin içinde geçer (şerit de zemin
rengiyle çizilir); `--brand-on-accent` yalnız `color` bildiriminde, `--brand-edge` yalnız kenarı
çizen özellikte. WL-5'in slot testi seçicilere ek olarak bu özellik eşleşmesini okur — yalnız
seçiciye bakan bir test `.tap-button{color:rgb(var(--brand-accent))}`'i geçirirdi.

**Neden 4,5 ve 3 değil.** Tap düğmesinin metni 20 px / 400 (ölçüldü). WCAG'ın "büyük metin"
eşiği 24 px normal ya da ~18,66 px kalındır; 20 px normal büyük metin değildir, 4,5:1 gerekir.

**Beş fonksiyon, saf** (`internal/brand/accent.go` — **WL-2**):

- `ParseAccent` — kanonik biçim: altı büyük harf hex, `#` yok. DB CHECK'i aynı biçimi ister
  (WL-1).
- `OnColor` — accent üstündeki metin: paper ve ink'ten **kontrastı yüksek olan**.
- `Check` — en iyi metin kontrastı `< 4,5` ise **RED** (`ErrAccentIllegible`).
- `Edge` — accent ile porcelain arasındaki kontrast `< 3` ise düğmeye **2 px ink kenar**
  (WCAG 1.4.11; ink on porcelain 14,32:1). Gerekçe: düğmenin sınırı zeminden ayırt edilmeli.
  Kenarın mekanizması düğmenin kutusunu değiştirmez (bugün düğmenin kenarlığı yok; ör. iç
  gölge) — marka ayarlamamış tenant'ın computed style'ı bugünküyle aynı kalmalı (**WL-5**).
- `Suggest` — aynı ton, açıklığı düşürerek `Check`'ten geçen en yakın renk; deterministik
  (→ **WL-2**: her çıktısı `Check`'ten geçer, özellik testi).

**Sınırlar — palet değerlerinden hesaplandı (2026-10-02):**

| Büyüklük | Formül | Değer | Tasarım özündeki |
|---|---|---|---|
| paper metin ≥ 4,5 | `L ≤ (L_paper+0.05)/4.5 − 0.05` | **0,1789826956…** | 0,1790 |
| ink metin ≥ 4,5 | `L ≥ 4.5(L_ink+0.05) − 0.05` | **0,2368152180…** | 0,2368 |
| OnColor dönüm noktası (paper = ink) | `√((L_paper+0.05)(L_ink+0.05)) − 0.05` | **0,2062727…**; orada en iyi kontrast **4,0208** | — |
| porcelain'e karşı ≥ 3 | `L ≤ (L_porcelain+0.05)/3 − 0.05` | **0,2542173…**; üstündeki aralıkta 3'e ulaşan değer yok (fonksiyon L = 1'de en çok 1,1505) | — |

Değerler formülün çıktısıdır; yazılı hâlleri **kesiktir** (`…`). Red bandı formülle
tanımlıdır: `paper sınırı < L < ink sınırı`; iki uç **geçer**. Kesik yazımı literal olarak
kullanmak kapıyı bozar, ölçüldü (2²⁴ rengin tamamı sayıldı):
- dört haneli 0,1790 / 0,2368 "dahil" okunursa **1 092** renk 4,5'e ulaşmadan geçer (ör.
  `#008384`, 4,49995:1); `L = 0,1790`'da paper 4,49966:1, `L = 0,2368`'de ink 4,49976:1;
- altı haneli 0,178983 / 0,236815 bile **14** renk geçirir (paper 9, ör. `#22864B`
  4,4999988:1; ink 5, ör. `#1E93A0` 4,49999…:1).

**Normatif:** testte ve kodda sınır literal yazılmaz, palet sabitlerinden hesaplanır; WL-2'nin
*"L sınırları 0,1790 ve 0,2368 dahil"* kabulü *"hesaplanan iki sınır dahil; her iki yanındaki en
yakın hex'le sınanır"* diye okunur. Kapı, 24 bitlik renklerin **1 949 736**'sını (%11,62)
reddeder. *(WL-2 notu, 2026-10-03: kodda L sınırı ne literal ne hesaplanmış olarak durur;
karar, palet sabitlerinden hesaplanan kontrast oranının 4,5 ve 3 eşikleriyle
karşılaştırılmasıdır — bu, paletten hesaplanmış sınırla aynı kümeyi verir. Sınırları
paletten türeten taraf testtir. Ayrıntı ve ölçüm: aşağıdaki WL-2 notu.)*

**Kabul edilen accent'in üç sınıfı** (sınırlar yukarıdaki formüllerin değeri): `L ≤ paper
sınırı` → paper metin, kenar yok (porcelain'e karşı ≥ 3,98) · `ink sınırı ≤ L ≤ porcelain
3:1 sınırı` → ink metin, kenar yok · `L > porcelain 3:1 sınırı` → ink metin + ink kenar.

| Renk | L | paper | ink | Sonuç |
|---|---|---|---|---|
| `#808080` | 0,215861 | 3,88 | 4,17 | **red** |
| `#E0457B` | 0,215336 | 3,88 | 4,16 | **red** |
| `#DA291C` | 0,165751 | **4,78** | 3,39 | geçer, paper metin |
| `#FFC72C` | 0,622887 | 1,53 | **10,56** | geçer, ink metin + ink kenar |
| tappa-green `#1F5C41` | 0,083273 | **7,73** | 2,09 | geçer (varsayılan) |
| tomato `#BE3D2A` | 0,144518 | **5,30** | 3,05 | geçer, paper metin |
| saffron `#D98E2B` | 0,342721 | 2,62 | **6,16** | geçer, ink metin + ink kenar |

Tasarım özündeki dört örneğin dördü de bu hesapla tutar.

**WL-2 notu (2026-10-02 — uygulama ve ölçüm; bu bölümün kuralı değişmedi).**
`internal/brand/accent.go` ve `accent_test.go`:
- **İmzalar.** `Color{R, G, B}` ve `Hex()` (kanonik `RRGGBB`) · `ParseAccent` kanonik yazımı
  kabul eder, başka yazımı `ErrAccentSyntax` ile reddeder (tema rotası, saklanan satır; ölçülen
  ret listesi `TestAccent_ParseAcceptsOneSpellingPerColour`'da) · **altıncı fonksiyon
  `NormalizeAccent`**: editörün `<input type="color">` değeri `#rrggbb` küçük harfle gelir;
  tek `#` ve küçük harfi kabul eder, çıktısının `Hex()`'i kanoniktir (WL-7 handler sınırında
  kullanır) ·
  `Check(c) (Fill, error)`: `Fill{Accent, Text, Edge}` §4'ün üç değişkenini taşır, red
  `(Fill{}, ErrAccentIllegible)` · `OnColor` eşitlikte paper'ı seçer; iki kontrast dönüm
  noktasında eşittir ve dönüm noktası red bandının içindedir.
- **Normatif cümle (*"testte ve kodda sınır literal yazılmaz, palet sabitlerinden
  hesaplanır"*) nasıl karşılanır** (2. tur, 2026-10-03; 3. turda düzeltildi). *Kodda:* L
  sınırı ne literal ne hesaplanmış olarak durur. Kontrast oranları palet sabitlerinden
  hesaplanır; `Check` en iyi metin oranını 4,5 eşiğine, `Edge` porcelain oranını 3 eşiğine
  doğrudan uygular; `OnColor` eşik uygulamaz, paper ve ink oranlarını birbiriyle
  karşılaştırır. Sınır formülleri `Check` ve `Edge` karşılaştırmalarının L'ye göre çözülmüş
  hâlidir (ör. paper oranı ≥ 4,5 ⇔ `L ≤ (L_paper+0.05)/4.5 − 0.05`), dönüm noktası formülü de
  `OnColor` karşılaştırmasının; yani kararlar paletten hesaplanmış sınırlarla aynı kümeyi
  verir. *Testte:* yukarıdaki tablonun dört değeri, `tailwind.config.js`'ten okunan paletle bu
  formüllerden türetilir, literal yoktur; test bu ADR'nin yazdığı kesik değerleri ve red
  sayısını o türetimle karşılaştırır (`TestAccent_TheADRPrintsTheBoundariesThePaletteYields`).
  2²⁴ yinelemede (x = 0 … 2²⁴−1) `Check`, doğrudan oran ve türetilmiş sınır aynı kararı verdi; bir
  rengin bir sınıra en küçük L mesafesi 4,87e-11, dönüm noktasına 1,41e-08
  (`TestAccent_EveryColourAgreesWithTheBoundariesThePaletteImplies`; kapsam
  enstrümantasyonu açıkken her 61. rengi tarar — gerekçesi testte ölçümle).
- **Sınırların iki yanındaki en yakın renkler**
  (`TestAccent_TheNearestHexEitherSideOfEachComputedBoundary`): paper `6E7B44` geçer /
  `7D5BEC` red · ink `1E93A0` red / `8D76DA` geçer · kenar `B268EC` kenarsız / `8F8A7A`
  kenarlı. Taranan 2²⁴ rengin hiçbiri bir sınırın tam üstünde değil; *"iki uç geçer"* bu
  komşularla sınanır.
- **`Suggest`'in okunuşu.** *"Aynı ton, açıklığı düşürerek"* HSL'de okundu: ton ve doygunluk
  sabit, açıklık iner (Sass `darken()`); sonuç bu yolda `Check`'ten geçen en parlak
  renktir, geçen renk değişmeden döner. Yol tam sayılarla hesaplanır: float aritmetiği iki
  kanalın aynı noktada yuvarlandığı yerde yolda olmayan bir renk önerebiliyor (`007EC6` için
  ders kitabı float bisection'ı `007AC1`, bu kod `007AC0` —
  `TestSuggest_ThePathIsTextbookHSL`). Bu ADR'ye göre koyulaştırır, açmaz; WL-2'nin scratch
  ölçümü: red renklerin 947 258'i (%48,6) L'de ink sınırına paper sınırından yakın, bunlarda
  öneri girdiden belirgin koyu olabilir (ör. `1E93A0` → `1A818D`). Açma yönü bu bölümün
  değişikliği olur — WL-2 kartında devredildi.

**Aynı fonksiyon iki tarafta** (CLAUDE.md §5'in `internal/netx` deseni): yazma tarafında
domain accent'i yeniden `Check` eder (→ **WL-4**: `ErrAccentIllegible`; form okunaksız
renkte yeniden render + önerilen hex, yazma yok — **WL-7**); okuma tarafında tema rotası
`Check`'ten geçmeyen hex'e 404 verir ve sayfa varsayılana düşer (→ **WL-5**: 200/404 matrisi
kanonik, küçük harf, geçersiz ve red bandı vakalarıyla). Okuma tarafı gerekli, çünkü palet
sabitleri değişirse daha önce kaydedilmiş bir accent kapıdan düşebilir.

### 4. Enjeksiyon — durumsuz tema rotası; CSP değişmez

- `GET /brand/theme/{HEX}.css` — gövde **yalnız** `:root{--brand-accent:R G B;
  --brand-on-accent:R G B;--brand-edge:…}` (ondalık kanal değerleri; kullanıcı metni CSS'e
  girmez). DB okumaz, kimlik doğrulamaz, tenant verisi taşımaz; kurucusuna havuz verilmez
  (`/healthz` emsali, `router.go:99`, `:151-156`). Yalnız kanonik **ve** `Check`'ten geçen hex
  200, gerisi 404. Başlıklar: `Content-Type: text/css; charset=utf-8`, `Cache-Control: public,
  max-age=31536000, immutable`, `X-Content-Type-Options: nosniff` (→ **WL-5**: kurucu havuz
  almıyor; başlıklar birebir; gövde yalnız üç özellik).
- **CSP değişmez:** tema bir dış stil dosyasıdır ve ölçülen yedi politikanın yedisi
  `style-src 'self'` taşır. `style=`, `<style>` ve `'unsafe-inline'` eklenmez (bugün sayıları
  0).
- Tailwind'e üç token: `brand`, `on-brand`, `brand-edge` — `rgb(var(--…) / <alpha-value>)`;
  `:root` varsayılanları: accent tappa-green, metin paper, kenar yok → marka ayarlamamış
  tenant'ta computed style bugünküyle aynı (→ **WL-5**, CDP ölçümü).
- Tema `<link>`'i **yalnız** accent slotu olan sayfalara: tap ekranı ve panel kabuğu (§2).
- Logo bir `<img>`'dir; `img-src 'self'` **yalnız `<img>` render eden sayfanın** politikasına
  eklenir (`tapCSPFor(hasLogo)`, `adminCSPFor(hasLogo)`; emsal `landingCSPFor`) — ayrıntı
  ADR 0024 §5 (→ **WL-6**: "sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır" testi,
  panel + tap). `Tap.render` tap, sonuç ve tap-problem yanıtlarında ortak olduğundan
  politika render başına hesaplanır.

**WL-5 notu (2026-10-03 — uygulama ve ölçüm; bu bölümün kuralı değişmedi).** Kod:
`internal/brand/theme.go` (`ThemeCSS`), `internal/handler/brandtheme.go` (`BrandTheme`,
`NewBrandTheme`), `cmd/tappa/main.go` (rota `httpx.NewRouter`'a verilir), `tailwind.config.js`,
`web/static/css/input.css`. Tema `<link>`'i bir sayfaya eklenmedi (WL-8, WL-9): `web/templates` diff'i
boş. Ölçüm ortamı: darwin/amd64, Go 1.27.1 (staticcheck Go 1.26.7), Tailwind v3.4.17,
headless Chrome 154.0.8037.93. Bu notta satır numarası yok; kurallar seçici adıyla anılır
(`.stamp`, `.stamp--*`, `.tap-button`, `.btn`). Bir testi anan cümle o testin aşağıdaki PART I
maddesine bağlıdır; mutasyon kimlikleri (H…, C…, X…) WL-5 kartının mutasyon tablosundadır.
- **Gövde `brand.ThemeCSS`'te**, `Check`'in `Fill`'inden. `Check`'in reddettiği accent için
  `ThemeCSS` gövde vermez (`""`, `ErrAccentIllegible`). `ThemeCSS` bir `Color` alır, rotanın
  dizgesini almaz: gövde fonksiyonun sabit metni ve `Fill` baytlarının ondalık yazımıdır. Gramer:
  `--brand-accent` ve `--brand-on-accent` `R G B`; `--brand-edge` `R G B` (ink) ya da `none`.
- **`Edge == false` iken `--brand-edge: none`** (WL-2 devri). `none` bir renk değildir. Bugün (WL-5)
  onu okuyan kural `.tap-button`'ın `box-shadow: inset 0 0 0 2px rgb(var(--brand-edge)/1)`'idir;
  `none` ile bu bildirim hesaplanan değer anında geçersiz olur ve `box-shadow` ilk değerine,
  `none`'a düşer. Chrome 154'te markasız düğmenin `background-color`, `color` ve `box-shadow`'u WL-5
  öncesiyle aynı ölçüldü (bir kez; §3'ün *"computed style bugünküyle aynı"* şartı). Kenarı accent'in
  kendi rengiyle çizen aday `box-shadow`'u ve köşelerde 36 pikseli değiştirdi (ölçüldü).
- **Kenarın mekanizması iç gölge** — CDP'de ölçülen temaların hepsinde düğme kutusu 358×64,
  `border` 0.
- **Token'lar `colors` altında değil, ADR 0023 §3'ün izin verdiği özelliğin altında:**
  `backgroundColor.brand`, `textColor['on-brand']`, `boxShadowColor['brand-edge']`, değerler
  `rgb(var(--…) / <alpha-value>)`. İki gerekçe: `colors` dokuz token'lık paletin bloğudur ve WL-2'nin
  Go kopya testi onu satır satır hex olarak okur; ve şablona yazılan `text-brand`, `border-brand`,
  `bg-on-brand`, `ring-brand-edge`, `text-brand-edge` derlenmiş CSS'te kural üretmedi (ölçüldü, S1).
- **Varsayılanlar** `input.css` `@layer base`'te bir `:root` kuralı; derlenmiş hâli
  `ThemeCSS(tappa-green)` ile bayt-aynıdır (PART I madde 13). `.tap-button` artık `bg-brand
  text-on-brand` + iç gölge okur; bugünkü yedi `class` kullanımı aynı kuralı alır.
- **Rota** (davranışı PART I madde 2–8): `/brand/theme/` + `ParseAccent`'in kabul ettiği hex +
  `.css`, sorgusuz ve `Check`'ten geçen accent 200; ret `http.NotFound`. Sorgu reddi: gövde sorguya
  bağlı değil, kabul etmek bir yıl önbelleklenen tek gövdenin URL'lerini çoğaltırdı. **HEAD**
  `/healthz` ve `/readyz` emsaliyle kabul. Chi'nin tanımadığı PROPFIND'in 405'i `Allow` taşımadı
  (bir kez ölçüldü; test yalnız durumu okur).
- **Rota ve gövde sürümü.** URL rengi adlandırır; gövde ayrıca palete, `ThemeCSS`'in biçimine ve
  `Check`'in eşiklerine bağlıdır ve tarayıcı `immutable` gövdeyi bir yıl yeniden sormaz. Bunlardan
  biri değişince yeni bir rota verilmesi bir **kuraldır, kod incelemesinin konusudur**; testin bu
  kuraldan tuttuğu PART I madde 8'dedir, tutmadığı sınır (2)'dedir.
- **Slot tablosu** (PART I madde 11): bugün `.tap-button`, üç değişkenle. Panel şeridinin sınıfı
  WL-8'de yalnız `--brand-accent` ile girer. Şablona çıplak yazılan bir accent yardımcısı `app.css`'e
  seçicisi slot tablosunda olmayan bir kural derler ve `TestCompiledCSS_BrandVariablesOnlyInTheirSlots`
  onu raporlar (ölçüldü: sınıf özniteliği, şablon yorumu, var olan bir yardımcı — C3, C4, S2). Test
  adları CSS kaçışlarını çözerek ve yorumları düşürerek okur (4. ve 5. tur). Bu test, ayrıştırıcısının
  okuduğu kurallar için, *"nerede ve hangi özellikte okunur"* ve *"nerede, kaç kez tanımlanır"*
  sorularını cevaplar; `brand` sözcüğünün dosyada geçtiği yerleri geçiş golden'ı tutar (aşağıda).
  `app.css` dışından gelen bir yol (ikinci bir stil dosyası, betikten yazılan stil) iki testin de
  dışında.
- **Geçiş golden'ı** (PART I madde 14 ve 15). Elle yazılmış ayrıştırıcının görmediği bir yazım 4.
  turda (kaçışlı ad) ve 5. turda (tanımdan önce yorum) bulundu; bu yüzden `brand`'in geçtiği yerler
  ayrıca ayrıştırıcısız sayılır. `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` derlenmiş
  `app.css`'te önce süslü paranteze çözülen kaçışları (`\{`, `\}`, `\7d `) U+FFFD ile değiştirir —
  tarayıcı için onlar bir adın harfidir, bloğun kenarı değil; 6. turda E1, E1c, E2 ve F4 bu adım
  olmadan yeşildi — sonra metni iki kez okur: yazıldığı hâliyle ve kalan kaçışlar çözülmüş hâliyle.
  İki okumada da `brand` sözcüğünün harf büyüklüğünden bağımsız her geçişini bir *yer* olarak sayar:
  geçişteki süslü parantez derinliği ve önceki `}`'den sonraki `}`'e kadar olan metin. Pencere
  küçük harfe indirilerek karşılaştırılır: golden'ın metinleri küçük harflidir, ve pencerede
  yalnız harf büyüklüğü değişen bir kural golden'ı yeşil bırakır (denetçinin CASE1'i). Bu yerlerin
  çoklu kümesi koddaki `themeBrandGolden` listesine birebir eşit olmalıdır. Golden yerlerin
  metnini tutar, anlamını değil: tanımın nerede olduğu madde 11 ve 13'ündür (E3 — tanımlar yalnız
  dengelenmiş, korunmuş bir yorumda — golden'ı yeşil, 11 ve 13'ü kırmızı bıraktı).
  **Bugünkü golden: dört yer, yedi geçiş** — `:root` varsayılanları (3), `.tap-button` ana kuralı
  (2: zemin ve etiket), `.tap-button` iç gölgesi (1) ve açılış sayfasının `.lp .plaque .p-brand`
  kuralı (1; bir değişken değil). WL-6'nın şablonları ve stil dosyası eklenmiş bir derlemede aynı
  dört yer ölçüldü. Seçim `--brand-` değil `brand`: daha geniştir; token yardımcılarının adlarını
  (`.bg-brand`, `.shadow-brand-edge`) ve yazıldığı hâliyle okumada kaçışlı tire yazımlarını da sayar.
  Bedeli: değişken olmayan bir yer (`.p-brand`) listede durur, ve `app.css`'e gelecekte giren her
  `brand` sözcüğü (ör. yeni bir `.brand-mark` sınıfı) bu testi kırmızıya çevirir; o düzenleme
  golden'ı da günceller. Pencere *önceki `}` → sonraki `}`*; bugünkü dört yerde bu, kuralın seçicisi
  ve bloğudur (ölçüldü). Derinlik ölçülen D1 biçimini ayırır (D1 kırmızı; derinliği çıkarılmış pinle
  yeşil — D1GU3). Bu listeye giren ya da listedeki kuralı değiştiren bir değişiklik (WL-8'in şeridi
  ilki) listeyi aynı düzenlemede günceller; o düzenleme incelemenin konusudur.
- **CDP (bir kez, pin değil):** gerçek `pages.Tap` render'ı `tapCSP` ile, `httpx.NewRouter` +
  `NewBrandTheme` üstünden, 390×844 DSF 3. Markasız: `background-color` **`rgb(31, 92, 65)`**,
  `color` **`rgb(255, 253, 244)`**, `box-shadow` `none`; tappa-green teması aynı; `FFC72C` →
  `rgb(255, 199, 44)` / `rgb(21, 34, 25)` / `rgb(21, 34, 25) 0px 0px 0px 2px inset`; `DA291C` → paper
  metin, kenar yok; 404 alan `808080` ve `1f5c41` bağlantıları → varsayılan. Düğmenin ekran
  görüntüsü WL-5 öncesiyle: markasız 0, tappa-green teması 0 piksel farkı; kenarı accent rengiyle
  çizen varyant 36 piksel (köşeler). Yöntem ve tablo: m10-platform.md → WL-5 kartı.
- **Sınırlar:** (1) Madde 11, 13 ve 14'ün testleri derlenmiş `app.css` ister: CI onu `make
  check`'ten önce derler (`.github/workflows/ci.yml` → *"Build the stylesheet (make css)"*), yani
  orada koşarlar; `make css` koşulmamış yerel bir koşuda SKIP ederler ve SKIP bir geçiş değildir.
  Madde 12 ve 15'in testleri `app.css` istemez. (2) Rota/gövde defterinden test yalnız madde 8'i
  tutar. Kaydın özeti yerinde yeniden yazılırsa yeşildir (ölçüldü, V5); silinen kaydı ve 15 rengin
  dışında kalıp 15'ini aynı bırakan bir değişikliği görmez. (3) `none` yalnız Chrome 154'te
  ölçüldü. (4) Slot testi derlenmiş `app.css`'in kurallarını okur — öğeleri, kaskadı, opaklığı ve
  başka stil dosyalarını değil; bir slot sınıfının slot olmayan bir öğeye yazılmasını görmez. (5) Tek bir
  sayfanın şablonuna yazılan tema bağlantısını DB'siz koşan testlerin hiçbiri kırmızıya çevirmedi
  (ölçüldü, L2); ortak `<head>`'e yazılanı `TestScreens_ReferenceOnlyOurOwnAssets` ve
  `TestTour_PointsOnlyAtItsOwnFlow` kırmızıya çevirdi (ölçüldü, L1) — tek sayfalık bağlantının ağı
  WL-9'un golden'ı. (6) Madde 1'in testi imzayı ve alanları görür; `serve`'ün ulaşabileceği paket
  düzeyi durum ya da fonksiyonu görmez (ölçüldü, A9) — kod incelemesinin konusu. (7) Madde 10'un
  testi sözdizimseldir (`main.go` AST'si): rota bir değişken ya da yardımcı üzerinden verilirse
  yanlış-kırmızı verir, `run()` dışındaki bir `NewRouter`'ı okumaz. (8) Rota bütçesiz (`/static`
  gibi): istek başına `ParseAccent` + `Check` + en çok 82 baytlık yazma; ADR bütçe istemiyor.
  (9) Golden bir yerin metnini ve derinliğini tutar, dosyadaki sırasını değil: aynı derinlikte, aynı
  metinle yeri değişen bir kural aynı yeri verir (ölçüldü: kenar kuralı `:root`'tan hemen sonraya
  taşındı, O1, yeşil; denetçinin F3 ve E16'sı yeşil). Derlenmiş dosyada iki kez yazılan bir kural
  kırmızıdır (O2; denetçinin F1/F2'si). Golden de sınır 4'teki gibi derlenmiş `app.css`'ten başka
  stil dosyası okumaz. 4. turun sınır 9'u (kenarın ikinci bir iç gölge okuması) kapandı: X5a madde
  14'ü kırmızıya çevirir. (10) **Kalıtım yolu:** accent'i `brand` sözcüğü ve değişken okuması
  olmadan taşıyan bir kural — bir slot öğesinden `inherit` ya da `currentColor` ile — hiçbir testte
  görünmez. Ölçüldü: `input.css`'e `.tap-button::after{content:"";position:fixed;inset:0;
  background-color:inherit}` (E5) yazılınca WL-5'in brand testleri ve handler'daki WL-5 kümesi yeşil
  kaldı; denetçi bunun accent'i bütün ekrana çizdiğini ölçtü. Bu yol kazara da yazılır: konumlanmış
  bir sözde öğe + `background: inherit` + ebeveynde unutulmuş `position: relative`. Denetçinin D9'u
  — dalga efekti olarak `.tap-button::after{content:"";position:absolute;inset:0;background:inherit;
  opacity:.15;pointer-events:none}`, `.tap-button`'da `position:relative` yok — Chrome'da ilk
  taşıyıcı bloğu (500×757) kapladı ve accent'i docket'in üstüne boyadı (paper `rgb(255, 253, 244)` →
  `rgb(249, 221, 211)`); brand testlerinin 9'u, handler testlerinin 22'si ve DB'siz 2601 testin
  tamamı yeşil kaldı. Kod incelemesinin konusu (WL-10); WL-9 tema bağlantısını tap ekranına
  eklediğinde bu yol bir tenant accent'iyle etkinleşir.
  (11) Yorum ya da dizge içindeki süslü parantez kaçış değildir ve nötrleştirilmez; bilerek
  dengelenmiş bir yazım golden'ı yeşil bırakır (ölçüldü: F4'ün kaçışlar yerine korunmuş yorumlardaki
  `}` ve `{` ile kurulmuş hâli, F5 — WL-5'in brand testlerinin hepsi yeşil). Tehdit modelinin
  dışında: kod incelemesi.
- **Güvenlik ve doğruluk iddiası (WL-5, üç parçalı).**
  - **Tehdit modeli:** Bu pinler `input.css` / `tailwind.config.js`'e kazara giren sapmaya karşıdır; tarayıcıyı atlatmak için bilerek yazılmış bir stil dosyası kod incelemesinin konusudur.
  - **PART I — her madde: test · beslenen girdiler · assert · o assert'i kıran mutasyon**
    (bu listede her assert'in yanında onu kıran mutasyon yazılı; *öncül* diye işaretli olanlar
    testin kendi girdilerini doğrular, ürün hakkında iddia değildir):
    1. `TestBrandTheme_TheConstructorTakesNothing` · `NewBrandTheme` ve `BrandTheme`'in derlenmiş
       tipi (`reflect`) · parametre sayısı 0 (H1); tek sonuç `*BrandTheme` (H20); alan sayısı 0
       (H2, H3).
    2. `TestBrandTheme_AnswersOnlyACanonicalLegibleHex` · `httpx.NewRouter(nil, nil,
       NewBrandTheme())`; kabul tarafı: §3 tablosunun beş kabul rengi, siyah, beyaz ve
       `brand.Check`'in kararının değiştiği üç yerin kabul tarafındaki komşuları; ret tarafı: iki
       red bandı komşusu, `808080`, `E0457B` ve testte listelenen yazım, uzantı, yol ve sorgu
       biçimleri · her kabul yolu 200 (H21) ve başlık haritası tam olarak üç başlık (H9, H10, H29,
       A3c);
       200 gövdesi gramere uyar, accent kendi baytları, etiket `Fill.Text`, kenar `Fill.Edge`'e göre
       ve `ThemeCSS(c)`'ye eşit (H12, H16, H17); her ret yolu bağlanmamış bir yolun 404'üyle durum,
       başlık haritası ve gövdede aynı (H4, H5, H6, H7, H8, H11, A3b); kabul edilenler iki `Edge` dalını
       kapsar (öncül).
    3. `TestBrandTheme_OnTheWireOnlyNetHTTPAddsHeaders` · gerçek sunucu; cevap bayt bayt, Go'nun
       HTTP ayrıştırıcısı olmadan okunur (`Connection` başlığı taşımayan bir keep-alive istek, sonra
       aynı bağlantıda ikinci bir istek); `1F5C41`, bağlanmamış bir yol ve `808080` · 200'ün durum
       satırı `HTTP/1.1 200 OK` ve sıralanmış başlık satırları tam olarak `Cache-Control`,
       `Content-Length` (gövde uzunluğu), `Content-Type`, `Date`, `X-Content-Type-Options` —
       üçü değerleriyle (H9, H10, H29, H22, H23, A3c); gövdesi `ThemeCSS`'inki; ret, bağlanmamış
       yolun 404'üyle durum satırı, başlık satırları (`Date` değeri hariç) ve gövdede aynı (H11,
       A3b); üç cevabın her birinden sonra bağlantı tam olarak ikinci isteğin 404'ünü — yönlendiricinin
       kendi bağlantısında verdiği 404 ile, `Date` değeri hariç, bayt bayt aynı — taşır ve kapanır
       (X8b, W1b).
    4. `TestBrandTheme_HeadIsGetWithoutTheBody` · madde 3'ün bayt bayt okuması, `FFC72C` ve
       `808080` · HEAD'in durum satırı ve sıralanmış başlık satırları GET'inkiyle, `Date` değeri
       hariç, aynı (H13, H22); HEAD'in başlık bloğundan sonra bağlantı tam olarak ikinci isteğin
       404'ünü taşır ve kapanır (X8, W2h, A3b, A3c); GET'in gövdesinden sonra da (X8b, W1b).
    5. `TestBrandTheme_OtherMethodsAre405` · iki yol; POST, PUT, PATCH, DELETE, OPTIONS, TRACE,
       CONNECT ve PROPFIND · yedi standart metot 405 ve `Allow` tam olarak GET, HEAD (A19);
       PROPFIND 405 (H25).
    6. `TestBrandTheme_TheOperatorHostGateLeavesItToTheCustomerHost` · `Config{OperatorHost}` ile
       kurulan yönlendirici, üç yol, müşteri ve operatör host'u · müşteri host'unda cevap ayarsız
       yönlendiricininkiyle durum, başlık haritası ve gövdede aynı (H26); operatör host'unda
       kapının bağlanmamış yol 404'üyle aynı (G1); müşteri host'unda `1F5C41` 200 ve üç başlık
       (öncül).
    7. `TestBrandTheme_TheAnswerDoesNotDependOnWhoAsks` · iki yol × dört istek giydirmesi
       (çalışan çerezi, panel çerezi, `Authorization`, yönlendirme + fetch başlıkları) · her
       giydirmenin cevabı çıplak isteğinkiyle durum, başlık haritası ve gövdede aynı (H14); çıplak
       cevapta `Set-Cookie` ve `Vary` yok (H27).
    8. `TestBrandTheme_ANewBodyNeedsANewRoute` · `chi.Walk` ile `Mount`'un kaydettiği yollar; 15
       rengin (§3 tablosunun yedisi, siyah, beyaz, madde 2'deki üç yerin iki yanındaki komşular)
       `ThemeCSS` cevaplarının sha256 özeti; `brandThemeShipped` defteri · `Mount` tek yol kaydeder
       (M8b); defterde hiçbir rota ve hiçbir özet iki kez geçmez (V3); bugünkü özet defterde tam
       bir kez geçer (V1, V2, T1); o kaydın rotası `Mount`'un kaydettiği yoldur (M8, V4).
    9. `TestTheme_TheBodyIsTheGatesFill` · §3 tablosunun yedi rengi, siyahtan başlayan her 4099.
       renk, 256 gri · `Check`'in reddettiğine gövde yok (H4); kabul edilene gramer ve kanonik
       ondalık (H17, H18), kendi baytları, `OnColor`'ın etiketi (H16), `Edge`'e göre ink ya da
       `none` (H15, M3); tablo satırlarında kapı tablonun etiket ve kenarıyla uyuşur (H28); tarama
       parçalarının ikisi de kapının dört cevabına rastlar (öncül).
    10. `TestBrandThemeWiring_RunMountsTheThemeRoute` · `main.go`'nun AST'si · `run()`'da tam bir
        `NewRouter` çağrısı (W2); onun argümanlarında tam bir argümansız `handler.NewBrandTheme()`
        (W1).
    11. `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` · derlenmiş `app.css` (yoksa SKIP) ·
        ayrıştırıcının okuduğu kurallar için (kaçışlar çözülmüş, yorumlar düşürülmüş)
        `themeSlotViolations` boş: `--brand-` bildirimlerin dışında — seçicide, at-rule başlığında,
        yorumda, blok içi yorumda — geçmez (K1, K2, E3); okuduğu bir tanım üst düzey `:root`'ta (C5,
        B1c3, E2) ve okuduğu tanımlarda her değişken bir kez (B1c5, C12, E3); bir okuma
        `themeSlots`'taki bir seçicide (C2, C3, C4, S2, C11, ST, X9a, X9b, X9c, X9d, E1, E1b, E1c, F4)
        ve `themeVariableProperty`'nin adlandırdığı özellikte (C1, C10); kenarı okuyan gölge `inset`
        (A16); `themeSlots`'taki her (seçici, değişken) çifti okunur (C9). Bu madde, ayrıştırıcının
        okuduğu kurallar için, *nerede ve hangi özellikte okunur* ve *nerede, kaç kez tanımlanır*
        sorularıdır.
    12. `TestThemeSlotScan_RefusesEachShapeItExistsFor` · sevk edilen şekil, tarama listesindeki
        bozuk şekiller, varsayılan listesindeki bozuk şekiller, iki üst düzey `:root` · tarama
        tablosunun değişken adları `ThemeCSS`'in bildirdikleriyle eşit (H30); sevk edilen şekil iki
        okumadan da geçer (öncül); tarama listesindeki her şekil kendi ihlal metniyle raporlanır
        (U1, U2, U5); varsayılan listesindeki her şekil tappa-green teması olarak okunmaz (U3);
        iki üst düzey `:root` reddedilir (U4).
    13. `TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme` · derlenmiş `app.css` (yoksa SKIP;
        adlar çözülerek eşlenir) · ayrıştırıcının bulduğu, `--brand-*` tanımlayan üst düzey `:root`
        kuralı tam bir tane (C12, B1c5, E2, E3); accent ve etiket paletin tappa-green ve paper'ı (C6,
        C7); `Edge(tappa-green)` false ve kenar `none` (C8); kuralın yazıldığı metin (yorumsuz seçici
        + `{`…`}`) `ThemeCSS(tappa-green)`'e bayt bayt eşit (X10). Bu madde *varsayılanlar ne*
        sorusudur; tanımların kaç kez ve nerede olduğu madde 11'in kural 2'sindedir.
    14. `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden` · derlenmiş `app.css`'in tamamı (yoksa
        SKIP); süslü paranteze çözülen kaçışlar U+FFFD'ye çevrildikten sonra yazıldığı hâliyle ve
        kalan kaçışları çözülmüş hâliyle · iki okumada da `brand`'in harf büyüklüğünden bağımsız her
        geçişinin (derinlik, küçük harfe indirilmiş çevreleyen metin) çoklu kümesi
        `themeBrandGolden`'a birebir eşit (B1c3,
        B1c5, K1, K2, X5a, ST, X9a, X9b, X9c, X9d, X11, C1, C2, C3, C5, C6, C10, C11, C12, S2, A16,
        X10, D1, O2, E1, E1b, E1c, E2, F4, GD, GD2). Bu madde yerlerin metnini ve derinliğini tutar,
        anlamını değil; tanımın yeri madde 11 ve 13'ündür (E3 bu maddeyi yeşil bırakır). Pencerede
        yalnız harf büyüklüğü değişen bir kural bu maddeyi yeşil bırakır (denetçinin CASE1'i).
    15. `TestThemeBrandScan_RefusesEachShapeItExistsFor` · golden'ın kendi kuralları art arda
        (sevk edilen şekil), testte listelenen on üç şekil, birer satırı eksik golden · golden bir
        yeri iki kez listelemez (öncül); golden kenar kuralını taşır (öncül; satırını silen GD2 bu
        testi de kırmızıya çevirdi); sevk edilen şekil fark vermez (öncül); listedeki her şekil
        fark verir (GU1, GU2, GU3, GU4, GU5, GU6); sevk edilen şekil birer satırı eksik golden'a
        karşı fark verir (GU4).
  - **PART II — pinler ve yakaladıkları:** PART I'in parantez içindeki mutasyonları — her biri
    kartın tablosunda, kırmızıya döndüğü testle ve koşulduğu `-run` kapsamıyla.
  - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

### 5. Co-brand (K3, K-2b)

Logo olan yüzeyde küçük bir **"taptime · punchless"** satırı kalır (tap, sonuç, panel). Tap
ve sonuç ekranındaki yeri **kullanıcı kararıdır (K-2b, 2026-10-02):** üstte tenant logosu,
**altında** co-brand satırı (10 px mono, ink/70), bugünkü büyük yeşil `taptime`'ın yerine.
Gerekçe, ölçülen iki olgu: aktivasyon ekranının GDPR cümlesi Taptime'ı **işleyen** olarak
adlandırır (`activate.templ:92-93`) — çalışanın telefonunu kaydettiği ekran ile her gün
gördüğü ekran aynı işleyeni göstermeli; ve landing'in çizdiği plaket `TAPTIME` basar
(`landing.templ:199`). Fiziksel plaket baskısı bu görevde **ölçülmedi** (skill'in plaket
bölümü baskıyı öneri olarak yazar). Ton: bugünkü `punchless` tonu (10 px mono, ink/70;
porcelain'de 5,70:1). Yerleşim ve boyut: skill → *"Tenant slotları (taslak)"*; kesinleşmesi
**WL-8/WL-9**.

**K-2b'nin aritmetiği (WL-9'un 16 px bütçesiyle).** Bugünkü başlık 28 px, co-brand satırı tek
başına 15 px (ölçüldü). Logo yuvası `H`, araları `g` ise yeni başlık `H + g + 15`, düğmenin
kayması `H + g − 13`. Kayma ≤ 16 px ⇔ `H + g ≤ 29`: ör. `g = 4` ile logo yuvası **≤ 25 px**.
40 px'lik bir yuva 31 px kaydırır ve WL-9'un bütçesini aşar. Hangisinin değişeceği (yuva ya
da 16 px) bir piksel bütçesidir, §9 sorusu değil: K-2b düzeni belirledi; sayıyı orkestratör
WL-9'da koyar (*Karar verilmedi*).

### 6. Tenant slotu olmayan öğeler — sayılı liste

Aşağıdakiler marka girdisinden **bağımsızdır**; tenant'ın accent'i ya da logosu bunlara
uygulanmaz (→ **WL-5**: *"tenant accent'i yalnız marka slotlarında"* testi — WL-5'te adı
`TestCompiledCSS_BrandVariablesOnlyInTheirSlots` oldu ve derlenmiş `app.css`'in kurallarını okur;
mutasyon: `.stamp`'e accent zemini → kırmızı, ölçüldü — §4 WL-5 notu):

1. Beş kaşe damgası (APPROVED / FLAGGED / REJECTED / IGNORED-RECORDED / TRAINING) ve
   durum→renk eşlemesi (`input.css` → `.stamp` ve beş `.stamp--*` kuralı).
   `TestCompiledCSS_StampWordIsInk` bunun bir yarısını okur: derlenmiş `app.css`'in iç içe süslü
   parantez taşımayan kurallarından seçici metni `\.stamp\b` desenine uyanların her `color:` değeri
   ink'i (`rgb(21 34 25`) içerir, böyle en az bir bildirim var, ve beş değiştirici (`approved`,
   `flagged`, `rejected`, `ignored`, `training`) bir `.stamp--…` seçicisinde geçer;
   `background-color` ve `border-color`'a bakmaz. Accent'in damgaya girmemesini
   `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` okur (§4 WL-5 notu, PART I madde 11).
2. tomato = hata / yıkıcı eylem; saffron = FLAGGED / geç kalma; `Notice` bileşeni.
3. Docket, perforasyon, `.docket-label`.
4. Panelin birincil ve yıkıcı düğmeleri, sekme vurgusu, form odak halkaları (tappa-green /
   ink kalır — K4: *"birincil butonlar yeşil kalır"*).
5. Odak göstergeleri: `.btn`'in ink halkası; tap düğmesinin tarayıcıdan gelen outline'ı
   (ölçüldü: Chrome 154'te rgb(0, 95, 204); kural renk koymadığı için accent onu taşımaz).
6. Sayfa zeminleri (porcelain, paper) ve metin tonları (ink, ink/70, ink/85).
7. Yazı tipleri.
8. Sonuç ekranına accent uygulanmaz (D-C: sonuçta yalnız logo; renk durumu anlatır). Logo
   olan tenant'ta Wordmark'ın yeşil `taptime`'ı yerini logo ve co-brand satırına bırakır (K-2b);
   damgaların ve metnin renkleri değişmez.
9. Onay kutusu ve radyo düğmelerinin `accent-color`'ı (`accent-tappa-green`, 5 dosyada 7
   kullanım) —
   `TestBrand_EveryNativeCheckboxAndRadioCarriesTheAccent`. *Ad çakışması:* oradaki "accent"
   CSS `accent-color`'dır, bu ADR'nin tenant accent'i değildir.
10. Kullanıcıya görünen metin: tenant adı dışında yeni kelime yok — tap ekranında yeni metin
    logonun `alt`'ıdır (WL-9); K-2b'nin co-brand satırı bugün Wordmark'ta duran iki kelimeyi
    (`taptime`, `punchless`) tek satırda taşır.

**"Birden çok vurgu rengi" (skill → Yapma) ile ilişki.** Panelde iki renk yan yana durur:
Taptime yeşili panelin kendi eylemlerinde, tenant rengi 4 px şeritte ve Account önizlemesindeki
tap düğmesinde. Bu, K4'ün bilinçli sonucudur: şerit dekoratiftir (`aria-hidden`, eylem ya da
bilgi taşımaz); önizlemedeki düğme bir eylem değil, tap ekranının görüntüsüdür (WL-7 onu
gönderilemez çizer — form değil). Tap ekranında:
- logo **varken** Wordmark'ın yeşil `taptime`'ı yerini logoya ve altındaki co-brand satırına
  bırakır (K-2b); ekranda yeşil öğe kalmaz, renk düğmede ve logonun piksellerindedir;
- logo **yokken** accent ayarlı tenant'ta başlıktaki `taptime` **ink** olur (K-2a,
  2026-10-02; ink on porcelain 14,32:1) — ekranda tek vurgu rengi tenant'ın düğmesidir;
- accent de logo da yoksa başlık bugünkü gibidir (yeşil `taptime`, porcelain'de 6,85:1).

### 7. CLAUDE.md §9 ile ilişki

§9'un *"Bu ekrana özellik eklemek istiyorsan önce sor"* kuralının sorusu üç kararla
cevaplandı:
- **D-C, 2026-09-24** — *"Logo + tap butonu tenant renginde; sonuç ekranında yalnız logo."*
- **K-2a, 2026-10-02** — logosu olmayan ama accent'li tenant'ın tap ekranında başlıktaki
  `taptime` ink olur (*"Ink (koyu) olsun"*, önerilen seçenek).
- **K-2b, 2026-10-02** — logo yükleyen tenant'ın tap ve sonuç ekranında üstte logo, altında
  küçük *"taptime · punchless"* satırı, bugünkü büyük yeşil `taptime`'ın yerine (*"Logo +
  küçük co-brand satırı"*, önerilen seçenek).

Üçünün birlikte kapsadığı değişiklikler — sayılı: (i) tap ekranı başlığında logo ve altında
co-brand satırı, (ii) tap düğmesinin zemininde accent (ve §3'ün ondan türettiği metin rengi
ile kenar), (iii) sonuç ekranı başlığında logo ve altında co-brand satırı, (iv) logosuz-accent'li
tap ekranında başlıktaki `taptime`'ın ink olması. Tap ve sonuç ekranlarındaki **başka** her
değişiklik — tur ekranlarına marka, sonuç ekranına renk, düzenlenebilir marka metni, yeni bir
metin ya da öğe — bu kararların **dışındadır** ve yeniden sorulur.

§9'un değişmeyen yarısı (→ **WL-9** kabulü): tek ekran, tek düğme, düğme metni *"Tap"*, en az
64 px; sonuç ekranında düğme ya da form yok (`TestTapPage_IsOneScreenOneButton`,
`TestResultScreen_HasNoButtonOrFormOnAnyVerdict`, `TestResultScreen_SaysExactlyThisAndNothingElse`
— sonuncusu `alt` metni için **bilinçli** güncellenir).

§9'un *"paletin dışına çıkma"* kuralı Taptime'ın kendi arayüzü için aynen geçerlidir. İki
sayılı istisna vardır: tenant accent'i (§2'deki dolgularda: tap düğmesi — tap ekranı ve Account
önizlemesi — ve panel şeridi) ve logonun pikselleri (bir görselin renkleri palet kuralının
konusu değildir; logo §2'deki dört yerde durur: tap ekranı, sonuç ekranı, panel kabuğu, Account
önizlemesi). CLAUDE.md'ye bu
cümleyi eklemek orkestratörün işidir (WL-12); bu ADR CLAUDE.md'yi değiştirmez.

**WL-9 notu (2026-10-03 — uygulama ve ölçüm; bu bölümün ve §2, §5, §6'nın kuralı değişmedi).**
Üç §9 kararının (D-C, K-2a, K-2b) kapsadığı dört değişiklik — (i) tap ekranı başlığında logo ve
altında co-brand satırı, (ii) tap düğmesinin zemininde accent, (iii) sonuç ekranı başlığında logo ve
altında co-brand satırı, (iv) logosuz-accent'li tap ekranında `taptime` ink — uygulandı; başka bir
tap/sonuç değişikliği yapılmadı. Orkestratörün piksel kararı (2026-10-03, §5'in *Karar
verilmedi*'si): **yuva 24 px, aralık 4 px** → kayma 24 + 4 − 13 = **15 px**, 16 px bütçenin
içinde. Kod: `web/templates/layout/brand.go` (`Logo`, `TapLogo`, `Brand`, `TapBrand(Logo, Theme)`;
alanlar dışa kapalı, kurucular biçimi doğrular) · `web/templates/layout/theme.go` (WL-8'in `Theme`'i,
`ThemeOf(brand.Color)`; WL-9 yalnız sıfır değer cümlesine tap kabuğunu ekledi) ·
`web/templates/layout/base.templ` (`shell` markayı alır; `documentHead(…, theme Theme)` temayı
`app.css`'ten hemen sonra yazar — WL-8'in imzası ve gövdesi; `brandHeader`; sonuç için
`BrandedPage(title, Logo)` — tema parametresi yok; tap için `PageWithScript(title, src, Brand)`;
`Page` değişmedi) · `web/templates/pages/tap.templ` (`Tap(v, b)`; `TapHeading`, `TapButtonFace`,
`tapForm`) · `web/templates/pages/result.templ` (`Result(v, logo)`) · `internal/domain/tenant/pagebrand.go`
(`PageBrand`, `ErrBrandUnread`, `pageBrandOf`, `Directory.ResultBrand`) ve `directory.go`
(`TapPageFacts.Brand`; `TapPage` markayı aynı transaction'ın son ifadesi olarak okur) ·
`internal/handler/tap.go` (`Page`'in markası, `tapBrandOf`, `logoOf`, `render(…, drawsLogo)` →
`tapCSPFor(drawsLogo)`) ve `checkin.go` (`resultLogo`) · `internal/httpx/ratelimit.go` (yalnız yorum).
Yeni sorgu, migration, bağımlılık yok; `go.mod`, `go.sum`, `sqlc.yaml`, `db/`, `internal/store/` diff'i
boş. Ölçüm ortamı: dev Postgres 17, `tappa_app`; yerel Go 1.27.1 (staticcheck ve tam DB'siz koşu Go
1.26.7); Tailwind v3.4.17; headless Chrome 154.0.8037.93.

- **Karar 1 — tap ekranının markası `TapPage`'in transaction'ında** (ADR 0024 WL-6 devri): çalışan
  ve mekân okumalarından sonra son ifade `GetTenantBrand`. Okuma ya da yorum başarısızsa (DB hatası,
  `logoRefOf`'un 14 yarım birleşimi, kanonik yazımda olmayan saklı accent) transaction bir iç sentinel'le
  geri alınır — başarısız ifade Postgres transaction'ını iptal eder, commit de düşerdi — ve `TapPage`
  selamlama + mekânla, **sıfır** markayla ve nedeni saran `ErrBrandUnread` ile döner
  (`ErrForeignLocation` sözleşmesi). Yabancı plakette (`ErrForeignLocation`: başka işletmenin duvarı
  ya da duvarsız plaket) marka **okunmaz**. Handler `ErrBrandUnread`'i ERROR log'la, varsayılan
  sayfayla karşılar; yarım logo satırı böylece okuma hatası gibi loglanır, `ErrLogoNotFound` olarak
  okunmaz.
- **Karar 2 — sonuç ekranının markası kendi transaction'ında, kayıt commit edildikten SONRA**
  (`Directory.ResultBrand`): kaydı yazan transaction'a bir görüntü okuması koymak, okumanın hatasıyla
  kaydı iptal ettirirdi (§4.6) ve `internal/domain/checkin` bu görevin kapsamı değil. Yalnız logo ve
  `alt` için işletme adı döner, accent hiç dönmez (D-C); logosuz işletmeye ad okunmaz. **Kayıt
  commit edildikten sonra onay isteğin iptalinden bağımsız, okuma sınırlı (2. tur S1, 3. tur B1):**
  `Checkin` isteğin İPTALİNİ bırakıp DEĞERLERİNİ koruyan bir bağlam kurar
  (`context.WithoutCancel(r.Context())`); marka okuması onun üstünde `resultBrandWait` = 2 s ile
  sınırlı, ekran onunla render edilir. Sonuçları: (a) commit sonrası onay ekranı isteğin süresinden
  ne kalmış olursa olsun tam gövdeyle yazılır; (b) `request_id` log'a ulaşır; (c) yanıt isteğin
  süresini (`httpx.RequestTimeout` = 30 s) en çok (`Record`'un son tarihten ne kadar sonra döndüğü) +
  2 s + render kadar aşar — test 22'nin üçüncü şekli (800 ms son tarih, commit 1 s'de) bu aşımı 2,2 s
  ölçer; (d) iptalin ikisi de düşer, son tarih de istemcinin kopması da: kopuk istemcinin isteği
  okumayı sınırına dek koşar ve ekranı kapanmış bağlantıya yazar (bedeli aşağıda). Neden: sınırsız okuma
  havuz doyunca isteğin bağlamı bitene dek bekliyor, render aynı bitmiş bağlamda çıkıp 200'ün
  üstüne BOŞ gövde yazıyordu (2. tur S1); 2. turun `min(2 s, isteğin kalanı)` sınırı kayıt isteğin
  son 2 s'sinde commit olunca aynı şekli bırakıyordu (3. tur B1 — denetçi sahte dizinle ve
  `pool_max_conns=1`'li gerçek Postgres'te ölçtü: 200, 0 bayt, kayıt 1). **Aşımın bedeli ölçüldü**
  (gerçek `http.Server`, üretimin ara katman sırası: `RequestID`, `AccessLog`, `Recoverer`, chi
  `Timeout`): `WriteTimeout` yokken chi'nin süresinden sonra yazılan 200 istemciye tam ulaşır, chi'nin
  geç `WriteHeader(504)`'ü sarılı yazıcıda yutulur (erişim kaydı 200, başka log yok). Onayı KESTİĞİ
  ölçülen iki sarmalayıcı var, ikisinde de erişim kaydı yine 200 der: handler'dan kısa bir
  `WriteTimeout` (istemci EOF) ve router'ın çevresinde handler'dan kısa bir `http.TimeoutHandler`
  (3. turun dar kapanış denetimi: 1,3 s ile istemci 503, 77 bayt, kayıt 1). `ReadTimeout` kesmez
  (ölçüldü). `cmd/tappa` ikisini de koymaz; AST pini (`TestServer_SetsNoWriteTimeoutThatCutsAConfirmation`)
  yalnız `WriteTimeout`'u görür, `TimeoutHandler` sınır 14'tür. **İstemci kopmasının bedeli ölçüldü**
  (dar kapanış denetimi): ≤ `brandWait` + render süren bir handler goroutine'i ve okuma beklerken
  havuzda bir bekleme yeri; 10 eşzamanlı kopuk POST'un her biri 2,00 s'de bitti, goroutine sayısı
  7'den 4'e döndü — sızıntı yok. Bu yolun kapasitesi önündeki tap kovalarıyla sınırlı: `ByAddress`
  3000 / 10 dk, `BySession` 300 / 10 dk (sınır 15). Bu ölçümler yüzünden okumayı
  `min(brandWait, kalan − pay)` ile sınırlayıp bütçe kısaysa atlayan tasarım gerekmedi — o tasarım
  süresi commit'te bitmiş bir tap'in logosunu da düşürürdü. 2 s'nin gerekçesi: okuma bir, logoluda
  iki PK okumasıdır (WL-8'in panel eşdeğeri seed'de < 1 ms), 2 s sağlıklı okumanın üç basamak üstü ve
  `httpx.RequestTimeout`'un 15'te biri (`TestNewTap_BoundsTheResultBrandRead` tam 2 s'yi ve en çok
  onda biri oranını tutar); ve plaketin önünde, kayıttan sonra "sayıldı" cümlesini bekleyen birinin
  bekleyeceği kadardır — ötesinde düşen logo olmalı.
- **Karar 3 — sonuç ekranında uyuşmazlık kuralı imzalı bağlamdan:** GET'in `ErrForeignLocation`
  cevabı iki gerçekten çıkar — plaketin işletmesi ve duvarı — ve ikisi de imzalı bağlamdadır
  (`TagTenantID`, `LocationID`; GET anında sunucunun ürettiği). Sonuç ekranı aynı soruyu aynı
  gerçeklere sorar: plaket oturumun işletmesinin bir duvarında değilse marka okunmaz. Bugün başka
  işletmenin plaketindeki kayıtlı tap bu ekrana ulaşmaz (`sys:tenant-mismatch` ilk guardrail, cevabı
  403 problem ekranı); kural o sıraya yaslanmamak için ekranın kendisinde.
- **Karar 4 — okunamayan marka hep-ya-hiç; kapının bugün reddettiği accent ise yalnız accent'i
  düşürür** (WL-8 ile birleştirmede panel kabuğunun kuralıyla eşitlendi — tek okuma tarafı kapısı,
  `accentOf`): bir parçası OKUNAMAYAN marka (DB hatası, yarım logo, kanonik olmayan accent) bütünüyle
  Taptime varsayılanına düşer ve ERROR log'lanır; saklı accent kanonik ama `brand.Check` onu BUGÜN
  reddediyorsa (palet ya da eşikler kayıttan sonra değişti) accent çizilmez — tema rotası da 404
  verirdi —, logo kalır ve WARN log'lanır (`PageBrand.AccentRefused`). İlk teslim reddedilen accent'i
  de hata sayıp logoyu düşürüyordu; aynı satırdan panel logoyu, tap ekranı Taptime'ı çizecekti.
- **Karar 5 — `alt` = işletme adı, mevcut sorgulardan:** tap ekranında
  `GetEmployeeActivationContext`'in zaten join ettiği `tenant_name`, sonuç ekranında
  `GetTenantClock`'un `name`'i. `TestStaffQueries_CarryAnExplicitTenantPredicate` ikisini de türetir.
- **Karar 6 — tema bağlantısı ortak `<head>`'de, panel kabuğuyla AYNI mekanizmayla, yalnız accent
  varken** (WL-5 devri; orkestratörün koordinasyon notu, 2026-10-03): WL-8'in `layout.Theme`'i
  (yalnız `brand.Color`'dan, `ThemeOf`; sıfır değeri hiçbir şey yazmaz) ve `documentHead(title,
  script, robots, theme)` imzası — `app.css`'ten hemen sonra. Tap kabuğu markanın temasını geçirir,
  sonuç kabuğu (`BrandedPage`) ve öteki kabuklar sıfır değeri. Markasız render'larda bayt değişmedi,
  bu yüzden L1'in iki testi (`TestScreens_ReferenceOnlyOurOwnAssets`,
  `TestTour_PointsOnlyAtItsOwnFlow`) değişmeden yeşil; birincisine logolu sonuç ekranı için bir alt
  test eklendi. WL-8 ile birleştirmede `theme.go` WL-8'inkidir; sıfır değer cümlesi *"her kabuk ama
  panelinki ve tap ekranınınki"* oldu (tap kabuğu da tema alır).
- **Karar 7 — `templ Tap`'in bölünmesi** (WL-7 bağımlılığı): `TapHeading(ad, mekân)` (docket) ve
  `TapButtonFace(TapButtonUse)` — `TapButtonSubmits` tap formunun submit düğmesi, `TapButtonPreview`
  (ve iki değerin dışındaki her değer) aynı sınıf ve kelimeyle `type="button"`, `data-tap-button`'sız.
  Bölme sonrası golden bayt-aynı; bunun için iki şekil gerekti — templ iki satır arasına boşluk yazar,
  elemanla bileşen çağrısı arasına yazmaz: docket ve form `templ.Join` ile birleşik, son alandan sonra
  açık bir `{ " " }` (ekranda etkisiz: form sütun düzeninde boşluğu düşürür). Golden ikisini de ilk
  denemede yakaladı.
- **Karar 8 — `PageWithScript` adı korundu:** tek çağıranı tap ekranı; marka ona açık parametre oldu.
  `BrandedPageWithScript` diye yeniden adlandırmak `admin.templ` ve `adminreset.templ`'deki yorumlarda
  ve paylaşılan `base.templ`'in panel/marketing yorumlarında bir dalga açardı (WL-8 paralel).
  `BrandedPage` sonuç ekranının kabuğu.
- **Karar 9 — tap tarafında yanıt başına bayt bütçesi YOK, gerekçeyle kabul** (ADR 0024 WL-6 sınır 12,
  tap yarısı): gerçek sayfada ölçüldü, bir tap soğukta 3, sıcakta 2 ücretli istek (sayfa, logo bir kez,
  düğme; sonuç ekranı sayfanın logo URL'ini adlandırır). Logoyu önbellekte tutan telefon onu digest
  başına bir kez çeker; bir yıllık `max-age` bir üst sınırdır, vaat değil — önbellek boşalırsa ya da
  logo değişirse yeniden çeker. **Kabul edilen sayılar (2. tur, güvenlik S2), istek başına maliyetle:**
  tap sayfası ~1,3 KiB, logo yanıtı 256 KiB'a kadar; 304 de aynı baytı DB'den okur, göndermez (ADR
  0024 WL-6 sınır 6). CANLI bir çalışan oturumu bilerek isterse 300 × 256 KiB = **75 MiB / oturum /
  10 dk**; on ya da daha çok canlı oturum taşıyan bir adres `tapAddressLimit` × 256 KiB = 3000 × 256
  KiB = **750 MiB / adres / 10 dk**. Oturumların çalınması gerekmez: kayıt herkese açık, kendini
  kaydeden işletmenin sahibi kendi çalışanlarını davet edip etkinleştirebilir; işletme başına
  davet/etkinleştirme tavanı ölçülmedi. Oturumsuz istek okumadan 404 alır. Bayt sayacı, ürünün tek
  kutsal ekranının önündeki sınırlayıcıya ikinci, durumlu bir boyut eklerdi; maliyet iki istek
  kovasıyla sınırlı ve bu büyüklükte KABUL edilir. `internal/httpx/ratelimit.go`'nun yorumu kararı
  taşır.
- **Karar 10 — tur/practice ekranlarında logo YOK** (§2: faz 2, *Karar verilmedi*): golden'daki
  `activate-tour-1..3`, `activate-done` bayt-aynı.

**Ölçüm — CDP (bir kez, pin değil; betikler scratchpad'de, repoya girmedi).** Gerçek `handler.NewTap`
(sahte önizleyici/dizin/oturum/kayıt), `httpx.NewRouter` + `NewBrandTheme`, gerçek `tapCSPFor`;
390×844 DSF 3 mobil; logo rotası 800 ms gecikmeli (yer ayrılmamışsa kayma görünsün diye); sonuç
ekranı formun gerçekten gönderilmesiyle.

| Varyant | başlık | düğme üst / yükseklik | kayma | düğme zemini / metni | logo kutusu (baytlardan önce → sonra) | layout-shift |
|---|---|---|---|---|---|---|
| markasız (WL-9 öncesi ve sonrası) | 28 | 214 / 64 | 0 | `rgb(31, 92, 65)` / `rgb(255, 253, 244)` | — | 0 |
| logo 512×128 | 43 | 229 / 64 | **+15** | aynı | 96×24 → 96×24 | **0** |
| logo 512×128 + `DA291C` | 43 | 229 / 64 | +15 | `rgb(218, 41, 28)` / paper | 96×24 → 96×24 | 0 |
| logo 128×512 | 43 | 229 / 64 | +15 | yeşil | 6×24 → 6×24 | 0 |
| logo 512×16 | 43 | 229 / 64 | +15 | yeşil | 179×24 → 179×24 (sütunun yarısı) | 0 |
| yalnız accent `FFC72C` (K-2a) | 28 | 214 / 64 | 0 | `rgb(255, 199, 44)` / ink + 2 px ink iç gölge | — | 0 |
| yalnız accent `DA291C` (K-2a) | 28 | 214 / 64 | 0 | `rgb(218, 41, 28)` / paper | — | 0 |
| başka işletmenin plaketi + logo + accent | 28 | 190 / 64 (mekânsız) | 0 | yeşil | — (`<img>` yok, tema yok) | 0 |
| KONTROL A: `width`/`height` yok, yuva yüksekliği yok | 44,75 | 249,75 | — | — | 0×0 → 179×44,75 | **0,0144 (1 kayma)** |
| KONTROL B: öznitelik var, yuva yüksekliği yok | 44,75 | 249,75 | — | — | 0×0 → 179×44,75 | **0,0144 (1 kayma)** |
| KONTROL C: öznitelik yok, 24 px yuva | 43 | 229 | — | — | **0×24** → 96×24 | 0 |

Her varyantta düğme tek, metni `Tap`, `type="submit"`; sonuç ekranında düğme ve form 0. Ölçülen
kontrast (porcelain zemin, sRGB kompozit): co-brand satırı ink/70 **5,70:1**, K-2a `taptime` ink
**14,32:1**, markasız `taptime` 6,85:1. Tema yalnız accent'li tap ekranında ikinci stil dosyası
olarak yüklendi; sonuç ekranlarının hiçbirinde tema yok, logolularda CSP `img-src 'self'` taşır,
logosuzlarda taşımaz. **Okuma:** sıfır kaymayı taşıyan sabit 24 px yuvadır; `width`/`height`
öznitelikleri logonun kendi kutusunu baytlar gelmeden kesinleştirir (C'de kutu 0 genişlikle açılır;
sağında bir şey olmadığı için kayma sayılmaz). Kontrol A/B ölçümün kaymayı görebildiğini gösterir.

**Ölçüm — ücretli istek, gerçek sayfayla** (`TestTapPage_ALogoTapIsTwoChargedRequestsWarmAndThreeCold`):
soğuk tap `/t`, `app.css`, `/brand/theme/DA291C.css`, `tap.js`, `/t/logo/<sha>`, `/api/checkin`
çekti → **3** ücretli; logosu önbellekte olan tap **2**. `internal/httpx/ratelimit_test.go`'nun
aritmetiği (sayfa + düğme + soğuk logo + bir yeniden deneme = 4) ölçümle tutar.

**Golden — 33 render** (`internal/handler/testdata/unbranded-golden/`, `df544c1`'den, değişiklikten
önce yazıldı; durum + CSP + gövde; tap ekranının imzalı bağlam değeri maskeli): tap ekranı 2
(`tap-page`, `tap-page-foreign-plaque`) · tap ailesinin problem ekranları 7 (`tap-problem-bad-url`,
`-unknown-plaque`, `-server-with-retry`, `-too-many`, `-stale-context`, `-post-unknown-plaque`,
`-another-employers-plaque`) · sonuç ekranı 14 (`result-ok-in-restaurant`, `-ok-out-restaurant`,
`-ok-in-production`, `-ok-out-production`, `-ok-in-other-trade`, `-ok-out-other-trade`,
`-ok-no-direction`, `-ok-in-practice`, `-ok-empty-note-no-venue`, `-flag-in`, `-flag-in-practice`,
`-reject`, `-ignored`, `-unknown-verdict`) · aktivasyon ailesi 10 (`activate-landing`, `-form`,
`-failure`, `-phone-in-use-form`, `-continue`, `-done`, `-done-without-session`, `-tour-1`, `-tour-2`,
`-tour-3`).

**`.tap-button` incelemesi (WL-5 sınır 10 devri):** derlenmiş `app.css`'te seçicisi `.tap-button`
olan kuralların hiçbiri sözde öğe (`::before`/`::after`) taşımıyor; `inherit` ve `currentColor` bu
kurallarda 0 (bir kez okundu, pin değil). Bu değişiklik `input.css`'e dokunmadı; derlenmiş dosyaya
yalnız `brandHeader`'ın dört yardımcısı girdi (`.w-auto`, `.max-w-[50%]`, `.object-contain`,
`.object-left`), yorumdan doğan kural 0.

- **Güvenlik ve doğruluk iddiası (WL-9, üç parçalı).**
  - **Tehdit modeli:** Bu pinler tap ve sonuç ekranının, paylaşılan kabuğun ve marka okumasının
    koduna kazara giren sapmaya karşıdır; tarayıcıyı ya da testleri bilerek atlatmak için yazılmış
    kod, kod incelemesinin konusudur.
  - **PART I — her madde: test · beslenen girdiler · assert · o assert'i kıran mutasyon:**
    1. `TestUnbrandedScreens_AreByteIdenticalToTheGolden` · yukarıdaki 33 render, markasız fake'ler,
       bağlı yönlendirici, `rec.Result()` · durum + CSP + gövde golden'a bayt eşit; golden dizini ile
       liste iki yönlü eşit; liste ve dizin tam olarak `unbrandedScreenCount` = 33 (2. tur, X22)
       (M08, M13, M23, M29, R3).
    2. `TestTapPage_TheBrandFillsTheTwoSlotsAndNothingElse` · dört marka şekli (yok, yalnız accent,
       yalnız logo, ikisi) · başlık şekle göre birebir (yeşil wordmark / ink wordmark / logo +
       co-brand); tema bağlantısı yalnız accent'te, tam bir kez ve `app.css`'ten hemen sonra; `<img`
       yalnız logoda tam bir; iki yuvanın dışında gövde markasız sayfayla bayt eşit; ekran metni
       birebir (yeni metin yalnız `alt`); tam bir `<button`, tap formunun submit'i, kelimesi `Tap`;
       CSP `tapCSP` ya da `+ img-src 'self'` (M04, M05, M06, M07, M12, M27, M28, M30, M31).
    3. `TestTapPage_TheLogoCarriesItsStoredBox` · beş kutu (512×128, 128×512, 512×16, 1×1, 512×512)
       · `width`/`height` saklanan kutu (M06, M07, M28, M30).
    4. `TestTapPage_AnotherBusinesssPlaqueShowsNoBrand` · `ErrForeignLocation` + logolu, accent'li
       marka · gövde markasız yabancı sayfayla bayt eşit, `/t/logo/` ve `/brand/theme/` 0, CSP
       `tapCSP` (M09).
    5. `TestTapPage_ABrandReadFailureRendersTaptimesPage` · `ErrBrandUnread` (neden: yarım logo) +
       markalı facts · 200, gövde markasız sayfa, CSP `tapCSP`, tam bir ERROR satırı nedeni taşır
       (M10, M11).
    6. `TestResultScreen_DrawsTheLogoAndNoAccent` · `ResultBrand` logo + accent, oturumun plaketi ·
       başlık logo + co-brand; `/brand/theme/` 0, tek stil dosyası; başlık dışı markasız ekranla
       bayt eşit; CSP `+ img-src`; okuma tam bir kez, oturumun işletmesi altında (M06, M07, M08, M28).
    7. `TestResultScreen_NoBrandWhereThePlaqueIsNotThisBusinesss` · bağlamda başka işletmenin plaketi
       (kayıtlı), duvarsız plaket (kayıtlı), başka işletme + `OutcomeForeignTenant` (403) ·
       `/t/logo/`, `/brand/theme/`, `<img` 0; CSP `tapCSP`; kayıtlılar markasız ekranla bayt eşit;
       marka okuması 0; kontrol: kendi plaketinde logo var (M14, M15).
    8. `TestResultScreen_ABrandReadFailureCostsOnlyTheLogo` · `ResultBrand` hata · 200, markasız ekran,
       CSP `tapCSP`, tam bir ERROR satırı (M16).
    9. `TestResultScreen_SaysExactlyThisAndNothingElse` (§7'nin adlandırdığı bilinçli güncelleme) · 11
       satır × {markasız, logo + accent} · ekran metni birebir; logoluda kabuk metni `Tapped —
       Taptime Kebab Factory Ltd taptime · punchless` (M07, M28).
    10. `TestScreens_RenderOnlyTheseElements` ve `TestScreens_ReferenceOnlyOurOwnAssets`'in logolu alt
        testleri · beş hüküm × practice · etiket kümesi = onay kümesi + `img`; referanslar tam olarak
        `app.css` + `/t/logo/<sha>` (M08).
    11. `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` · 34 render (+5: logolu/accent'li tap
        ve sonuç, uyuşmazlık) · img-src yalnız `<img` çizen yanıtta (M12, M13).
    12. `TestTapButtonFace_ThePreviewFaceCannotSubmit` · `TapHeading`, `TapButtonFace` üç değerle ·
        docket ve submit yüzü tap sayfasının baytları; önizleme ve bilinmeyen değer `type="button"`,
        veri özniteliği yok (M24, M25).
    13. `TestLayoutBrand_RefusesAnyOtherShape` · yedi bozuk logo; sıfır `Theme` ve `ThemeOf(DA291C)`;
        bir logo kontrolü · bozuk logolar sıfır değer, sıfır tema boş href, renk tek yazımıyla
        `/brand/theme/DA291C.css` (M01, M02, M03).
    14. `TestTapView_FieldCountIsTheSpec` · 3 alan (M26); `TestResultView_FieldCountIsTheSpec` 8 alan
        (değişmedi); `TestScreens_TakeTheBrandAsAnExplicitParameter` · iki imza (mutasyon koşulmadı).
    15. `TestTapPage_ALogoTapIsTwoChargedRequestsWarmAndThreeCold` · gerçek yönlendirici, üretim
        bütçeleri · soğuk 3, sıcak 2; soğuk tap `app.css`, tema, `tap.js` ve logoyu çekti (M27, M30,
        M31).
    16. `TestTapDB_ABrandIsDrawnOnlyOnTheBusinesssOwnPlaque` · gerçek Postgres, iki işletme, ürünün
        yazıcısıyla logo + accent · B'nin plaketinde tap sayfası ve düğmeden sonraki ekran (403):
        `/t/logo/`, `/brand/theme/`, `<img` 0, CSP `tapCSP`; A'nın plaketinde A'nın logosu, `alt`'ı ve
        teması, B'ninki değil; sonuç ekranında A'nın logosu, tema 0, CSP `+ img-src` (M19).
    17. `TestTapPageDB_TheBrandComesWithTheGreetingAndTheVenue` · gerçek Postgres · satır yok → sıfır;
        accent; logo + accent + ad; başka işletmenin duvarı ve duvarsız → sıfır + `ErrForeignLocation`
        (iki işletme de markalı); temizlenince sıfır (M18, M19).
    18. `TestTapPageDB_ABrandReadFailureKeepsTheGreetingAndTheVenue` · marka sorgusu gerçek bağlantıda
        `SELECT 1/0` ile iptal · `ErrBrandUnread` 22012'yi sarar; selamlama ve mekân korunur, marka
        sıfır; kontrol (M17, M21, M32).
    19. `TestResultBrandDB_TheLogoAndTheNameAndNeverTheAccent` · gerçek Postgres · logo + ad, accent
        asla; yalnız accent → sıfır; öbür işletme kendi cevabı (M22).
    20. `TestPageBrand_EveryReadFailureIsAnErrorNeverNoBrand` · dikiş · satır yok ve hepsi NULL sıfır;
        DB hatası, 14 yarım satır, kanonik olmayan accent hata (hiçbiri `ErrLogoNotFound` değil);
        kapının bugün reddettiği accent (`808080`, `E0457B`) logonun yanında hata değil, accent yok,
        `AccentRefused`, logo kalır (M21; birleştirmede A1, A2, A3).
    21. `TestTapPage_AnAccentTheGateRefusesTodayKeepsTheLogo` (birleştirmede eklendi) · `AccentRefused`
        logolu ve logosuz · tema 0; logoluda logo başlığı ve `+ img-src`; logosuzda markasız sayfayla
        bayt eşit; tam bir WARN, ERROR 0 (A4).
    22. `TestResultScreen_AStalledBrandReadStillSaysAllDone` (2. tur S1; 3. turda üç bütçe şekli, B1)
        · bağlamı bitene dek bekleyen sahte `ResultBrand`; isteğin süresi üretimdeki gibi chi
        `Timeout`'u (`httpx.RequestID` arkasında); üç şekil: sınırdan çok bütçe (1,5 s istek, 50 ms
        sınır), sınırdan az bütçe (kayıt 1,5 s'lik isteğin 1. saniyesinde commit, sınır üretimdeki
        2 s), süre commit'te bitmiş (800 ms istek, commit 1 s'de) · üçünde de 200, "All done", gövde
        markasız onay ekranıyla bayt eşit, CSP `tapCSP`; kayıt 1, okuma 1; tam bir ERROR satırı süre
        aşımını adlandırır ve isteğin `request_id`'sini taşır; süre = commit + okumanın KENDİ sınırı
        (az değil, en çok +1 s) (R1, R2, K2, K9, K11, W0, W1, W2).
    23. `TestNewTap_BoundsTheResultBrandRead` (2. tur S1; 3. turda N3) · `NewTap` · alan
        `resultBrandWait`'e eşit; sabit tam 2 s (Karar 2); `httpx.RequestTimeout`'un en çok onda biri
        (R4, K9, K10a).
    24. `TestResultScreen_ARecordCommittedAtTheDeadlineKeepsItsLogo` (3. tur) · 800 ms istek, commit
        1 s'de, sağlıklı okuma · logolu onay ekranıyla bayt eşit, CSP `+ img-src`, ERROR 0, ≤ 2 s —
        isteğin bağlamından türeyen okuma bitmiş bağlamı bulup logoyu düşürürdü (R2, R4, W0, W1, W2).
    25. `TestTapDB_ARecordedTapIsConfirmedInFullWhileThePoolIsHeld` (3. tur, B1) · gerçek Postgres,
        `pool_max_conns=1`'li havuz, gerçek kayıt; commit'ten hemen sonra havuzun tek bağlantısı
        tutulur; 1,5 s'lik istek (chi `Timeout`) · 200, tam onay ekranı (`<title>`, `</html>`), logo
        yok, CSP `tapCSP`; süre okumanın kendi sınırı (≥ 2 s, ≤ 3,5 s); kayıt 0 → 1; tam bir ERROR
        `request_id` taşır; kontrol: aynı havuz ve zincirde tutulmadan logo çizilir (K2, K8, K11, W0,
        W1, W2).
    26. `TestResultBrandDB_TheReadEndsWhenItsContextDoes` (3. tur, N1) · marka sorgusunun yerine gerçek
        bağlantıda `pg_sleep(3)` (DDL'siz), 300 ms'lik bağlam · `ErrBrandUnread`
        `context.DeadlineExceeded`'i sarar, sıfır marka, ≤ 1,8 s; kontrol: sınırsız bağlamla 3 s uyur ve
        süre aşımı olmayan bir hatayla döner (K8).
    27. `TestNewRouter_EveryRequestCarriesTheRequestTimeout` (3. tur, N3) · `NewRouter` üstünden bağlanan
        özellik · isteğin son tarihi `httpx.RequestTimeout` (K10b).
    28. `TestServer_SetsNoWriteTimeoutThatCutsAConfirmation` (3. tur, B1) · `cmd/tappa`'nın test dışı
        Go dosyaları, AST · `http.Server` literal'i ≥ 1; `WriteTimeout` anahtarı ya da seçicisi 0
        (WT1).
  - **PART II — pinler ve yakaladıkları:** PART I'in parantez içindeki mutasyonları — WL-9 kartının
    tablosunda, kırmızıya döndüğü testlerle: 32 mutasyon, 32'si kırmızı; son kod ve test sürümüne
    karşı tek koşuda, derleme hatası 0. WL-8 ile birleşik ağaçta birleştirmenin dokunduğu yerler
    yeniden koşuldu (kartın birleştirme notu): 13 + 6 + 4 mutasyon, hepsi kırmızı; M20'nin hedefi
    (`pageBrandOf`'taki `brand.Check` çağrısı) birleştirmede `accentOf`'a taşındı, yerine A1–A3.
    2. tur (dalın ucu `6d31015` üstünde): R1–R4 ve etkilenenlerin yeniden koşusu, kartın 2. tur notunda.
    3. tur: W0–W2, denetçinin K2, K8, K9, K10 (a, b), K11'i, WT1 ve R1–R4 yeniden — 14 mutasyon, 14'ü
    kırmızı, DB'li olanlar `.env`'le; kartın 3. tur notunda.
  - **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.
- **Sayılı sınırlar (WL-9).** (1) Golden yalnız listelenen 33 render'ı yakalar; yalnız durum, CSP ve
  gövde (öteki başlıklar değil); tap ekranının imzalı bağlam değeri maskeli. (2) CDP bir kez, Chrome
  154'te; pin değil. (3) Okunamayan marka hep-ya-hiç (logo da düşer, ERROR); kapının bugün
  reddettiği accent yalnız accent'i düşürür (WARN). (4) Sonuç ekranının uyuşmazlık kuralı GET anında imzalanan plaket
  gerçeğine bakar; plaket GET ile POST arasında (≤ 15 dk) başka işletmeye taşınırsa kayıt kararı
  `sys:tenant-mismatch`'in POST anındaki okumasıdır, logo kuralı bağlamınkidir. (5) Sonuç ekranı
  kayıttan sonra +1 transaction (1–2 PK okuması). (6) Bayt: oturum başına 10 dk'da 75 MiB, adres başına 750 MiB, kabul
  (Karar 9). (7) Uzun ince logo 24 px yuvada küçük çizilir (128×512 → 6×24 px, ölçüldü) — yuva
  kararının bedeli. (8) `.tap-button` incelemesi bir kez okundu; WL-5 sınır 10 (kalıtım yolu) aynen
  durur. (9) DB fikstürleri dev'de kalır. (10) Sonuç ekranının marka okuması kayıttan sonra en çok
  `resultBrandWait` = 2 s bekler; yanıt isteğin süresini en çok (`Record`'un son tarihten sonraki
  dönüşü) + 2 s + render aşar (Karar 2 (c); testte ölçülen en büyük aşım 2,2 s); sonradan bir
  `WriteTimeout` ya da `TimeoutHandler` eklenirse bu aşımdan uzun olmalı. Tap ekranının okuması (`TapPage`'in
  transaction'ı) ayrıca sınırlı değil — o okuma selamlamanın ve mekânın transaction'ında olduğu için
  takılan bir DB sayfanın tamamını bekletir, logoyu değil. (11) **Kırık logo:** logo isteği 404 olursa (ör. logo değişti, eski sayfa açık)
  tarayıcı 24 px yuvada kırık görsel simgesini ve `alt`'ı — işletme adını — kırpılmış gösterir;
  `alt` = ad kabul şartının bedeli, kod değişmez. (12) **Commit'ten önce biten süre** (WL-9
  değiştirmedi; 3. turun dar kapanış denetimi ölçtü): isteğin süresi `Record` dönmeden biterse cevap
  500 ve **0 bayt**, log'da ERROR `rendering the tap page failed err="context deadline exceeded"` —
  problem ekranı (`renderCheckinFailure` → `renderProblem`) bitmiş istek bağlamıyla render ediliyor;
  WL-9'dan önce de böyleydi. COMMIT sunucuda uygulanıp pgx'in bağlam hatası döndürdüğü belirsiz
  durumda boş 500'ün arkasında bir kayıt olabilir. Backlog'a devredildi (orkestratör). (13) WL-9'un
  testleri gerçek Postgres'te yalnız "sınırdan az bütçe" şeklini ölçer (kayıt ~0,1 s'de, 1,5 s'lik
  istek); "süre commit'te bitmiş" şekli testte yalnız sahteyle. Domain'e dokunmadan ölçülebilir —
  gerçek `Record`'u saran bir sarmalayıcıyla: 3. turun dar kapanış denetçisi üç şekli de böyle
  gerçek DB'de ölçtü, üçünde de 200 ve tam gövde (kanıt, pin değil). (14) **`http.TimeoutHandler`:**
  router'ın çevresine handler'dan kısa süreli bir `TimeoutHandler` konursa kayıtlı tap'in onayı yerine
  503 gider (ölçüldü: 1,3 s ile 77 bayt, kayıt 1, erişim kaydı 200); bugün yok, AST pini onu görmez —
  `main.go`'daki yorum ve bu sınır uyarır. `ReadTimeout` onayı kesmez (ölçüldü). (15) **İstemci
  kopması:** onay bağlamı iptalsiz olduğu için kopuk istemcinin isteği de okumayı sınırına dek koşar —
  ≤ `brandWait` + render süren bir goroutine ve okuma beklerken havuzda bir bekleme yeri (ölçüldü: 10
  eşzamanlı kopuk POST 2,00 s'de bitti, goroutine 7 → 4); aynı anda kaç tane olabileceği tap
  kovalarıyla sınırlı (adres başına 3000, oturum başına 300 / 10 dk), ayrı bir tavanı yok.
- **Devirler.** **WL-7:** önizleme `pages.TapHeading` ve `pages.TapButtonFace(pages.TapButtonPreview)`
  çağırır; başlık için `layout.brandHeader` dışa açılmalı ve panel rotası için `TapLogo`'nun yanına bir
  kurucu (`/admin/brand/logo/`) eklenmeli; önizleme img-src korpusuna; panel bayt kararı. **WL-8 /
  birleştirme (yapıldı):** iki görev tek mekanizma kullanır — `layout.Theme` ve `documentHead(…,
  theme)`; `shell` markanın temasını, `PanelWithScript` panelinkini geçirir; iki golden (bu notun
  33 render'ı ve WL-8'in panel golden'ı) birlikte yeşil; accent + logo tek PK okuması için
  `pageBrandOf` hazır. **WL-10:** bu notun üç parçalı iddiası, 32 mutasyon,
  sınırlar, `.tap-button` incelemesi. **WL-12:** skill *"Tenant slotları"*'ndan "taslak" kalkar ve
  ölçülen sayılar girer (24 px yuva, 4 px aralık, 15 px kayma, 5,70:1, 14,32:1); CLAUDE.md §9
  cümlesi; aşağıdaki *Karar verilmedi* maddesi kapandı; sınır 11'in kırık logo görünümü skill'e;
  aşağıdaki *Sayılı sınırlar* 1'in WL-9 ekinin ADR 0005 marka taklidi ekine girmesi.

### 8. E-posta (K8) — WL-11'e

Faz 1'de e-postada tenant markasından yalnız **ad** vardır, gövdede ve kaçışlıdır; gönderen
sabit `Taptime <no-reply@taptime.mt>`'dir (EM-K5). Tenant adı herkese açık kayıtla seçilir; bu
yüzden başlığa girmez (→ **WL-11**: `"X\r\nBcc: y"` biçiminde bir tenant adı ek başlık
üretmez; RFC 2047; `From`'un alan adı Taptime'ınkidir). WL-11 SES akışıyla (B) birlikte koşar.

### 9. Kapsam dışı (sayılı)

1. Tenant'a özel alan adı / alt alan adı — `cookies.go:143-149`'un dağıtım kısıtı ve plakete
   yazılan NDEF URL tabanı (`encode/session.go:1127-1134`) buna bağlı; değiştirmek çip
   yeniden encode'u ve çerez önekinin yeniden açılmasını ister.
2. Tenant yazı tipi.
3. Karanlık tema (skill: *"Karanlık tema yok"*).
4. Markalı fiziksel plaket.
5. Favicon / PWA manifest'i.
6. İkinci accent, gradient, tenant'ın düzenlediği metin (marka cümlesi bugün de
   düzenlenemez — `TestAccount_SaysTheMessagesCannotBeEdited`).
7. Aktivasyon ekranında logo (K6).

## Güvenlik ve doğruluk iddiaları — üç parçalı

Her iddia üç parçadır. PART I'deki "ölçülecek" davranışların testleri henüz yoktur; adları
kendi görevlerinde konur, burada **tarifleriyle** yazılır.

**İddia A — tema rotası tenant verisi taşımaz ve veritabanına gitmez.**
- **PART I:** bugün rota yok (ölçüldü). WL-5'te, sürülen vakalarda ölçüldü (§4 WL-5 notu): rota kurucusunun imzası bir havuz
  ya da sorgu arayüzü almaz; 200 gövdesi `^:root\{--brand-accent:\d{1,3} \d{1,3} \d{1,3};
  --brand-on-accent:…;--brand-edge:…\}$` biçimine birebir uyar; kanonik olmayan, küçük harfli,
  geçersiz ve red bandındaki hex 404 alır.
- **PART II:** WL-5'in kurucu imzası testi (havuz parametresi yok;
  `TestBrandTheme_TheConstructorTakesNothing`) · gövde biçimi testi
  (`TestTheme_TheBodyIsTheGatesFill`) · 200/404 matris testi
  (`TestBrandTheme_AnswersOnlyACanonicalLegibleHex`). Yakaladıkları: kurucuya havuz eklenmesi; gövdeye üç özellik dışında bir
  şey girmesi; `Check`'ten geçmeyen bir hex'in 200 alması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia B — marka ayarlamamış tenant'ın sayfası bugünküyle aynıdır.**
- **PART I:** WL-9'da ölçülecek: marka satırı olmayan bir tenant'ın tap ve sonuç ekranının HTML
  gövdesi ve CSP başlığı, değişiklik öncesi golden dosyayla bayt-aynı. *(WL-9'da ölçüldü — §7'nin
  WL-9 notu, PART I madde 1: 33 render.)* WL-5'te bir kez ölçüldü
  (CDP, pin değil; §4 WL-5 notu): tap düğmesinin computed zemini `rgb(31, 92, 65)`, metni `rgb(255, 253, 244)`.
- **PART II:** WL-9'un golden testi — **WL-9 kartında adıyla ve sayısıyla listelenen**
  fikstürler (tap ekranı; sonuç ekranının hüküm × yön × iş türü × practice varyantlarından
  seçilenler; aktivasyon ailesi), her biri HTML + CSP · WL-5'in derlenmiş-CSS testi
  (`TestCompiledCSS_RootDefaultsAreTheTappaGreenTheme`) —
  `app.css`'teki `:root` bildiriminde `--brand-accent` ve `--brand-on-accent` varsayılanlarını
  okur (`TestCompiledCSS_StampWordIsInk` emsali; `app.css` yoksa o da atlanır). Yakaladıkları:
  markasız tenant'ın **golden fikstürü olan** render'larının HTML'inde ya da CSP'sinde bir bayt
  farkı; `:root` varsayılanlarının tappa-green / paper'dan sapması (derlenmiş-CSS testi).
  Fikstürü olmayan bir varyant bu testin dışındadır.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C — derlenmiş `app.css`'te marka değişkenlerini okuyan kurallar §2'nin slot sınıflarında ve
§3'ün özelliğindedir; `brand` sözcüğünün geçtiği yerler bir listeye eşittir.** (WL-5'in 6. turunda
PART I'e daraltıldı; ilk başlık *"accent §6'nın sayılı öğelerine uygulanmaz"* idi. Accent'i değişken
okumadan, kalıtımla taşıyan bir kural bu iddianın dışındadır — §4 WL-5 notu, sınır 10.)
- **PART I:** WL-5'te ölçüldü (§4 WL-5 notu, madde 11 ve 14): ayrıştırıcının okuduğu kurallarda
  marka değişkenlerini okuyanlar §2'nin slot sınıflarındadır ve §3'ün özellik kuralına uyar
  (`--brand-accent` yalnız `background-color`'da); `brand`'in derlenmiş dosyada geçtiği yerler
  `themeBrandGolden`'a eşittir; `.stamp`'e accent zemini eklemek ve `.tap-button`'ın `color`'ına
  `--brand-accent` yazmak (iki mutasyon) iki testi de kırmızıya çevirir.
  WL-9'da ölçülecek: markalı (accent'li) bir tenant'ın sonuç sayfasında tema `<link>`'i 0 —
  D-C'nin *"sonuç ekranında accent yok"* yarısı. *(WL-9'da ölçüldü — §7'nin WL-9 notu, PART I madde
  6 ve 16.)*
- **PART II:** WL-5'in slot testi (`TestCompiledCSS_BrandVariablesOnlyInTheirSlots`) · geçiş golden'ı
  (`TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`) · `TestCompiledCSS_StampWordIsInk` — derlenmiş
  `app.css`'te `.stamp` seçicili kuralların `color:` bildirimlerini okur; zemin ve kenar
  renklerine bilerek bakmaz; `app.css` derlenmemişse atlanır (skip) ·
  `TestBrand_NoOffPaletteColourInAnySource` — şablonlarda ve `input.css`'te varsayılan Tailwind
  paleti ile keyfi renk değerli yardımcı sınıfları arar · WL-9'un sonuç sayfası testi (markalı
  tenant, tema `<link>` 0). Yakaladıkları: slot sınıfı dışında `--brand-*` okuyan bir kural ya da
  `--brand-accent`'in `background-color` dışında bir özellikte kullanılması (WL-5'in testi); damga kelimesinin renginin ink dışına çıkması; şablonlara palet dışı renk
  yardımcı sınıfı girmesi; sonuç sayfasına tema bağlantısı eklenmesi.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia D — bir tenant'ın markası başka tenant'ın çalışanına gösterilmez.**
- **PART I:** WL-9'da ölçülecek: A tenant'ının oturumu B'nin plaketinde tap sayfasını açınca ve
  o tap'in sonuç sayfasında gövdede `/t/logo/` 0 isabet ve tema `<link>`'i yok; eşleşen
  tenant'ta sonuç sayfasında logo var; logonun kendi rotasındaki izolasyon ADR 0024 iddia D.
  *(WL-9'da ölçüldü — §7'nin WL-9 notu, PART I madde 4, 7, 16, 17.)*
- **PART II:** WL-9'un uyuşmazlık testi (tap + sonuç) · WL-1'in RLS testi (`WHERE`'siz A
  bağlamı B'yi 0 görür) · WL-6'nın bayt-aynı 404 testi. Yakaladıkları: uyuşmazlıkta tap ya da
  sonuç sayfasında markanın render edilmesi; RLS'in kapanması; başka tenant'ın sha'sının
  404'ten ayırt edilebilmesi.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

## Elenen seçenekler

- **Tam tema** (birden çok renk, yazı tipi, zemin) — durum→renk eşlemesi ve AA hesapları
  renk çifti başına çoğalır; §9'un kitchen docket kimliği tenant başına dağılır.
- **Satır içi stil / sayfa başına `<style>`** — CSP'ye `'unsafe-inline'` ya da nonce ister;
  bugün yedi politikanın hiçbiri onu taşımıyor (ölçüldü).
- **Tenant başına derlenmiş CSS** — Tailwind standalone CLI derleme zamanındadır (CLAUDE.md
  §1); çalışma zamanında derleme ya da tenant başına dosya ikinci bir yapı hattı olurdu.
- **Tema rotasının tenant id'siyle DB'den okuması** — rotayı kimlik ve bütçe taşıyan bir rotaya
  çevirir, tenant'lar arası önbelleği bozar; hex adresli durumsuz rota global önbelleklenir.
- **Sonuç ekranında accent** — D-C eledi (renk orada durumu anlatır).
- **Kapısız accent + "otomatik metin rengi"** — 24 bitlik renklerin %11,62'sinde ne paper ne
  ink 4,5'e ulaşır (ölçüldü); otomatik seçim o bantta AA'yı karşılamaz.

## Sayılı sınırlar ve kabul edilen riskler

1. **Marka taklidi.** Herkese açık kayıtla tenant olan biri başka bir işletmenin logosunu ve
   rengini kullanabilir. Plaket uyuşmazlığı `sys:tenant-mismatch` ile kayda geçer; logo içeriği
   denetlenmez. ADR 0005'e eklenmesi **WL-12'nin kabulüne bağlandı** (bu görev ADR 0005'i
   düzenlemez; `cmd/tappa/adr0005_test.go`'nun sayımları ekleme ile birlikte güncellenir — ADR
   0020'nin 8. sınırıyla aynı emsal).
   *(WL-9 eki, 2026-10-03, güvenlik S3:)* taklit yalnız başka bir işletmenin logosu ve rengi değildir.
   **Logonun pikselleri** Taptime'ın durum kelimelerini ya da damgasını taklit edebilir (tap
   ekranında "✓ Tapped in", REJECTED ya da FLAGGED bir sonuç ekranında "APPROVED"); **`alt`**, görsel
   yüklenmezse gösterilen serbest metin işletme adıdır. Kim: yalnız işletmenin sahibi, kendi
   çalışanlarına karşı. Sonuç: çalışan düğmeye basmadan işin bittiğini sanarsa kayıt oluşmaz.
   Hafifletenler: 24 px yuva ve `max-w-[50%]` (logo küçük ve başlıkta), 64 px'lik tek "Tap" düğmesi,
   sonuç ekranında logonun altındaki docket ve kaşe damgası. WL-12'nin ADR 0005 marka taklidi ekine
   bu iki biçim de girer.
2. **Durum renklerine yakın accent kabul edilir.** tomato `Check`'ten 5,30 ile geçer. §2'ye
   göre accent bir kaşe damgasıyla aynı ekranı panel bölümlerinde paylaşabilir: 4 px şerit
   olarak, Account bölümünde ayrıca önizlemenin tap düğmesi olarak. Tap ekranında damga yok,
   sonuç ekranında accent yok. Yakınlık kuralı yok — *Karar verilmedi*.
3. **Çok açık accent şeritte görünmez.** `#EEEEEE` porcelain'e karşı 1,01:1; şerit dekoratif
   olduğu için kabul. Aynı renk tap düğmesinde ink kenar alır.
4. **Tema URL'i accent'i açık eder.** Hex tenant verisi değildir; rota durumsuzdur, aynı hex
   isteyen herkese aynı gövde gider.
5. **Soğuk önbellekte sayfa başına +1 istek** (tema stil dosyası; sonra `immutable`). Logo
   isteğinin bütçe etkisi ADR 0024 §5.
6. **Açık logo açık zeminde kaybolur** — WL-7'de uyarı, ret değil. Uyarının zemini logonun
   oturduğu zemin olmalı: bugünkü başlık satırı porcelain üstünde (ölçüldü); plan *paper*
   yazıyor — sapma kart düzeltmesinde.

## Karar verilmedi

- ~~Logosuz ama accent'li tenant'ın tap ekranı başlığı~~ → **karara bağlandı: K-2a,
  2026-10-02** (`taptime` ink; §6, §7). Bu maddenin iki okuması bu ADR'nin önceki taslağında
  bekleyen soru olarak duruyordu.
- ~~**WL-9'un 16 px bütçesi ile K-2b'nin logo yuvası**~~ → **karara bağlandı: orkestratör,
  2026-10-03** — yuva 24 px, aralık 4 px, kayma 15 px (ölçüldü; §7'nin WL-9 notu).
- Tur / practice ekranlarında logo (faz 2) ve o ekranlardaki altı `.tap-button`'ın accent alıp
  almayacağı — §9 sorusudur, faz 2 açılınca sorulur.
- E-postada *"X via Taptime"* gönderen adı (yalnız VIES-doğrulanmış tenant koşuluyla, WL-11) ve
  logo (faz 2, CID, ≤32 KiB varyant; uzak URL elendi — izleme pikseli).
- Accent'in durum renklerine yakınlık kuralı (sınır 2).
- Logo yuvasının kesin boyutları — skill taslağı aritmetikle öneri yazar; WL-8/WL-9 ölçer. *(Tap ve
  sonuç ekranı: WL-9 notu, 24 px yuva; panel: WL-8.)*

## Sonuçlar

- WL-1…WL-12 bu ADR'yi ve ADR 0024'ü normatif kaynak alır.
- **Orkestratöre — CLAUDE.md güncellemesi gerekecek (WL-12):** §9'a tenant slotu cümlesi
  (§7); §3 dizin haritasına `internal/brand`. Bu ADR CLAUDE.md'ye dokunmaz.
- **Bilinçli güncellenecek mevcut testler** (plan listesi): `TestResultScreen_SaysExactlyThisAndNothingElse`
  (`alt` metni) · `TestLandingFacts_EveryDeclaredFactIsDerivedAndClaimed` içindeki
  `FactNoBulkImport` türetimi (ADR 0024 §6) · `TestBrand_` ailesi · panel CSP ↔ script
  karşılığı (`TestPanelScreens_ScriptsAndPolicyAgreeAndReachNoThirdParty`).
- Askı (OP-15): `ProtectWriting` içindeki `suspensionGate` marka yazma rotalarını kapsar
  (m10-platform.md §2, akışlar arası çakışma çözümleri).
- **WL-12'nin kabulüne eklenenler:** ADR 0005'e marka taklidi eki (sınır 1, `adr0005_test.go`
  sayımlarıyla birlikte) · skill'in *"Tenant slotları (taslak)"* bölümünden "taslak"ın kalkması.

## WL-8 notu (2026-10-03 — panel kabuğu; uygulama ve ölçüm. §2'nin panel satırı ve §4'ün kuralları değişmedi)

**Kod.** `db/queries/branding.sql` (`GetTenantPanelBrand`; `internal/store` üretimi) ·
`internal/domain/tenant/brandread.go` (`PanelBrand` tipi, `BrandReader.PanelBrand`,
`panelBrandOf`, `accentOf`) · `internal/handler/panelbrand.go` (`panelBrands`, `panelBrand`,
`panelBrandView`) · `internal/handler/review.go` (`chrome()` markayı okur) ·
`internal/handler/adminlogin.go` (`brands` alanı ve kurucu parametresi; `renderPanel`;
`renderScripted` krom alır) · 15 kabuk sayfasının 22 render çağrısı `renderPanel`/
`renderScripted`'e (`account.go`, `accountactions.go`, `accountpassword.go`, `anomalies.go`,
`billing.go`, `billingactions.go`, `dashboard.go`, `employees.go`, `employeeactions.go`,
`locations.go`, `locationactions.go`, `manualentry.go`, `policies.go`, `policyactions.go`,
`reports.go`, `review.go`, `transactions.go`) · `internal/handler/brandlogo.go`
(`adminLogoHref`) · `web/templates/layout/theme.go` (`Theme`, `ThemeOf`) ·
`web/templates/layout/base.templ` (`documentHead`'e `theme Theme`; `Panel`/`PanelWithScript`
tema alır; öteki dört kabuk sıfır değeri geçer) · `web/templates/pages/panelbrandview.go`
(`PanelBrand`, `PanelLogo`, `PanelLogoOf`, `DrawsLogo`) · `web/templates/pages/adminview.go`
(`PanelChrome.Brand`) · `web/templates/pages/admin.templ` (`PanelShell`, `PanelShellWithScript`,
`panelChrome`, `panelBrandHeader`) · `web/static/css/input.css` (`.panel-stripe`) ·
`cmd/tappa/main.go` (okuyucu `NewAdminAuth`'a). Testler: `internal/handler/panelbrand_test.go`,
`panelbrand_golden_test.go`, `brandlogo_test.go` (img-src derlemi genişledi),
`internal/domain/tenant/panelbrand_db_test.go`, `internal/brand/theme_test.go` (slot tablosu ve
golden), `internal/db/branding_test.go` (sorgu sayısı ve seçim listesi),
`cmd/tappa/storekeyshape_test.go` (envanter). Migration, bağımlılık yok (`db/migrations`,
`go.mod`, `go.sum`, `sqlc.yaml` diff'i boş). Tap/sonuç şablonları ve `tap.go`, `checkin.go`,
`directory.go` dokunulmadı; **ortak `base.templ`'e en küçük değişiklik** (yukarıda; WL-9 aynı
dosyada çalışıyor). Ölçüm ortamı: dev Postgres 17 (paylaşılan, 00029'da), `tappa_app`; yerel
go1.27.1 darwin/amd64, staticcheck go1.26.7; Tailwind v3.4.17; headless Chrome 154.0.8037.93;
ayrı git worktree, taban `df544c1`.

### Kararlar

1. **Tenant adı için tek ifade, iki PK.** Oturum adı taşımaz (`adminauth.Resolved`:
   `FullName`, `Role`), `GetTenantBrand` `tenants.name`'i seçmez. İki okuma (`GetTenantClock`
   + `GetTenantBrand`) kabulün *"tam +1 PK okuması"*nı bozardı; `TouchAdminSession`'a
   `tenants` join'i (sıfır ek okuma) elendi: panelin her istekteki yetki ifadesini bu görev
   değiştirmez. Pin bu kadar: kromun **marka okuyucusuna istek başına bir çağrısı** (handler
   testleri sayar) ve **okuyucunun tek ifadesi** (domain testi sayar); bir bölümün başka
   okumaları sayılmaz (1. tur, F5). `GetTenantPanelBrand`: `FROM tenant_branding JOIN tenants`, ikisi de PK ile,
   `WHERE tenant_id = @tenant_id AND tenants.id = @tenant_id` (her tablo tenant'ı adlandırır;
   `tenant_id` nitelemesiz — `tenants`'ta o sütun yok, `internal/db`'nin kuşak deseni o yazımı
   okur). `logo` seçilmez. **EXPLAIN ANALYZE** (seed tenant Kebab Factory, `tappa_app`, tenant
   bağlamında, sıcak; satırlı ölçüm geri alınan bir transaction'da): satır yok → `Index Scan
   using tenant_branding_pkey` (rows=0), `tenants_pkey` *never executed*, yürütme 0,071 /
   0,111 ms; accent + logolu satır → iki `Index Scan` (`tenant_branding_pkey`, `tenants_pkey`),
   yürütme 0,082 / 0,090 / 0,156 ms, generic plan 0,102 ms; planlama ≤0,161 ms. Hepsi < 1 ms.
2. **Marka satırı yoksa satır yok.** Sorgu `tenant_branding`'den başlar: markasız işletme adı
   okumaz, kabuk adı çizmez — markasız HTML bugünküyle bayt-aynı kalır.
3. **Başlık kuralı.** *Markalı* = gate'ten bugün geçen accent **ya da** logo. Markalıda
   Wordmark'ın yerine `panelBrandHeader`: 4 px şerit (yalnız accent geçiyorsa), logo (varsa),
   ad (Space Grotesk kalın, ink), altında co-brand satırı *"taptime · punchless"* (10 px mono,
   büyük harf, ink/70). Yalnız accent'li işletmede de ad + co-brand satırı çizilir; yeşil
   `taptime` kalkar (K-2a'nın panel karşılığı; panelin yeşili eylemlerde kalır — K4).
   Kabuğun geri kalanı (oturum bloğu, çıkış, sekmeler, başlık) değişmez.
4. **Tema `<link>`'i `<head>`'de, `app.css`'ten hemen sonra.** `layout.documentHead` bir
   `theme layout.Theme` alır; `Theme` yalnız `brand.Color`'dan kurulur (`ThemeOf`: `/brand/theme/`
   + kanonik hex + `.css`), sıfır değer hiçbir şey yazmaz. Yalnız iki panel kabuğu
   (`Panel`, `PanelWithScript`) işletmenin temasını geçer; `shell`, `MarketingWithScript`,
   `Auth`, `OperatorWithScript` sıfır değeri. `<body>` içinde `<link>` elendi: geçerli HTML ama
   ürünün tek `<head>` bileşeninin dışında ikinci bir stil kaynağı olurdu.
5. **CSP yolu krom'dan.** `renderPanel(chrome)` → `adminCSPFor(chrome.DrawsLogo())`;
   `renderScripted(chrome)` → `logoImagePolicy(adminScriptedCSP, chrome.DrawsLogo())`.
   `DrawsLogo` şablonun `<img>`'i çizdiği yüklemin kendisidir; politika ve işaretleme aynı
   değerden karar verir. `render` (false) kabuksuz sayfalarda kalır: picker, problem
   sayfaları, docket parçası.
6. **Logo kutusu: 32 px yükseklik, en çok 192 px genişlik**, oran korunur, saklanan boyuttan
   büyük çizilmez (küçük logo büyütülüp bulanıklaşmaz); kenarlar en yakın piksele, en az 1.
   Kutu `width`/`height` özniteliklerinin kendisidir — yeni CSS sınıfı gerekmedi (Tailwind
   preflight'ın `img{height:auto}`'su yüksekliği öznitelik oranından hesaplar). 512×128 →
   128×32 (Chrome'da ölçüldü).
7. **Bugün gate'ten geçmeyen kaydedilmiş accent** (palet değişirse): okuma tarafı
   `ParseAccent` + `Check` (`accentOf`). Geçmeyen → `AccentRefused`, şerit ve tema bağlantısı
   yok (rota da aynı rengi 404'ler), WARN log; logo varsa logo + ad çizilir.
8. **Hata yolları sayfayı düşürmez, sessizce "marka yok" da sayılmaz (§4.6).** Okuma hatası,
   yarım tanımlı logo (`logoRefOf`'un 14 birleşimi — ADR 0024 WL-6 devri), kanonik olmayan
   accent ve 64 küçük hex olmayan digest → ERROR log (tenant_id + hata; testte ad ve digest
   aranır ve bulunmaz; sürücünün hata metni ölçülmedi) + markasız krom. Hiçbiri
   `ErrLogoNotFound`'a çevrilmez. Reddedilen accent'in WARN satırında ad, accent ve digest
   aranır ve bulunmaz (1. tur, F2).
9. **Panel başlığındaki logonun `alt`'ı boş (`alt=""`)** — orkestratör kararı, 1. tur. Ad,
   logonun hemen yanında görünür metindir (§2: panelde tenant adı görünür metin); ada eşit bir
   `alt` ekran okuyucuda iki kez okunur ve logo yüklenemezse adı kutunun içine basıp sayfayı
   adın satır sayısı kadar iter. Skill'in *"alt = tenant adı"* kuralı adın görünmediği tap
   ekranı için yazıldı; orada geçerli kalır (WL-9). Ölçüm: aşağıda, Ölçümler.
10. **AdminChoose ve panel problem sayfası Taptime'da; giriş sayfası yalnız iki noktada
    ölçüldü.** `TestPanelBrand_TheSignInFamilyStaysTaptime`'in ölçtüğü: iki işletmeli giriş →
    picker yürüyüşünde okuyucu 0 kez çağrılır; picker ve problem sayfası Wordmark taşır, şerit,
    `<img>`, tema/logo yolu, co-brand yoktur, politika `adminCSP` literal'i; giriş sayfasının
    (`/admin/login`: kendi `layout.Auth` kabuğu ve `adminLoginCSP`'si — Wordmark taşımaz, kendi
    "taptime" paneli var) politikası img-src adlandırmaz. Parola sıfırlama ailesi (kendi
    `render`'ı) bu testte koşulmaz. `layout.Page` ve `layout.Auth` kabukları sıfır temayla
    golden'da bayt-aynı.

### Ölçümler

- **Markasız = bugünkü bayt.** 24 bileşen render'ı `df544c1`'inkiyle uzunluk + sha256 eşit
  (`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`; liste: `PanelShell` dokuz bölüm
  sekmesinde, rozetin dört hâli, tanımsız sekme, kaçış gerektiren ad, `PanelShellWithScript`,
  `AdminDashboard`, ve `documentHead`'e ulaşan öteki altı kabuk — `Page`, `PageWithScript`,
  `Marketing`, `MarketingWithScript`, `Auth` iki hâliyle, `Operator`, `OperatorWithScript`).
  Digest'ler aynı test dosyasının `df544c1` dışa aktarımında koşulmasıyla alındı.
- **CSS.** Taze `app.css`'in `df544c1` derlemesine farkı tek kural:
  `.panel-stripe{height:.25rem;--tw-bg-opacity:1;background-color:rgb(var(--brand-accent)/var(--tw-bg-opacity,1))}`.
  `themeSlots`'a `.panel-stripe: --brand-accent`, `themeBrandGolden`'a bu kural (beşinci yer)
  aynı düzenlemede girdi; şablon yorumlarından kural doğmadı.
- **Chrome 154 (bir kez, pin değil; 1024 geniş, DSF 1, gerçek router):** şerit 4 px yükseklik,
  992 px (sütun); DA291C pikselleri yalnız şeritte (3 968 = 992 × 4); logo kutusu 128 × 32;
  tappa-green birincil düğmede ve sekme işaretinde kaldı (K4). **Kırık logo (1. tur, F1):**
  `alt=""` ile logo 404 alınca yüklenmiş hâline göre farklı pikseller **yalnız** logonun
  128 × 32 kutusu (4 096 px), kutunun dışında **0 px** — 18 karakterlik adla ve 112
  karakterlik adla (`/admin`, 1024 geniş). Karşılaştırma için `alt` = ad iken: 16 karakterlik
  fixture'da alt metni iki satıra sarılıp içeriği ~5 px itiyordu; denetçinin 114 karakterlik
  adında kutu 128 × 216 oldu ve içerik +145 px kaydı (denetçinin ölçümü).
- **Kontrast:** ad ink on porcelain 14,32:1; co-brand ink/70 on porcelain 5,70:1; şerit metin
  taşımaz (§3 tablosu: dekoratif, şart yok). 1. turdan (F6) beri adın ve co-brand satırının
  paragrafları **tam sınıf listeleriyle** pinli: adın paragrafı renk sınıfı taşımaz (sayfanın
  ink'i), co-brand satırı Wordmark'ın tagline sınıflarını (ink/70) — renk sınıfı eklemek
  `TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection`'ı kırmızıya çevirir (R06).
- **EXPLAIN ANALYZE:** karar 1.
- **img-src'nin `true` yarısı ilk kez ölçüldü** (ADR 0024 WL-6 sınır 2 kapandı): img-src derlemi
  29 → 56 render, 18'i logoyu çizer ve img-src adlandırır.

### Güvenlik ve doğruluk iddiası (WL-8, üç parçalı)

- **Tehdit modeli:** Bu pinler panel kabuğuna, render yoluna, `input.css`'e ve okuma sorgusuna
  kazara giren sapmaya karşıdır; bir testi atlatmak için bilerek yazılmış kod kod incelemesinin
  konusudur.
- **PART I — her madde: test · beslenen girdiler · assert · o assert'i kıran mutasyon:**
  1. `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8` · sabit girdili 24 bileşen render'ı
     (liste kabul 2'de) · her birinin uzunluğu ve sha256'sı `df544c1`'inkine eşit; derlem ve
     golden aynı adları taşır · W09.
  2. `TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome` · gerçek router, owner oturumu, dokuz
     bölüm × dört işletme (marka yok; okuma düşer; yalnız reddedilen accent; büyük harf digest'li
     logo) · 200; politika literal'e eşit; Wordmark tam bir kez ve sekme çubuğundan önce; tek stil
     `app.css`, `<head>`'de; `<img`, şerit, tema/logo yolu, ad, co-brand yok; dört varyant bölüm
     başına bayt-aynı; marka okuyucusuna istek başına bir çağrı, oturumun işletmesiyle (bölümün
     başka okumaları sayılmaz); hata/digest ERROR, ret WARN, satır başına `tenant_id`, ad ve digest
     yok · W01, W03, W09, W20, W22, W23.
  3. `TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection` · aynı router, dokuz bölüm ×
     dört marka (accent + logo; yalnız accent; yalnız logo; logo + bugün reddedilen accent) ·
     politika img-src'yi tam logolu yanıtta adlandırır; stiller `app.css` ve (accent geçiyorsa)
     hemen ardından `/brand/theme/DA291C.css`, hepsi `<head>`'de; şerit sınıfı accent'te 1, yoksa
     0, ilk öğe ve `aria-hidden`; logolu yanıtta tek `<img>`: src, `width=128`, `height=32`, `alt`
     **var ve boş**; ad ve co-brand **tam sınıf listeleriyle** (adda renk sınıfı yok); Wordmark yok;
     tema bağlantısı ve başlık geri alınınca markasız sayfa bayt bayt; marka okuyucusuna istek
     başına bir çağrı; log yok — ret varyantında istek başına bir WARN ve log **adı (tamamı ve ilk 8
     karakteri), reddedilen accent'in hex'ini (808080), digest'i (tamamı ve ilk 8 hanesi)
     taşımaz** · W02, W03, W04, W05, W06, W07, W08, W09, W20, W21, W23, W24, W27, W28, W29, R01,
     R02, R03, R04, R05, R06.
  4. `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage` (WL-6'nın, genişledi) · 56 render
     (27'si markalı panel) · her yanıtta `<img` ⇔ img-src; `<img` çizen render sayısı derlemdeki
     logolu panel render'ı sayısına (18) eşit ve > 0 · W02, W03, W04, W05, W06.
  5. `TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy` · `pages`'in üretilmiş Go'sundan
     türetilen kabuk sayfaları (15), `internal/handler`'ın test olmayan her `.go`'su, yedi kontrol
     şekli · adıyla geçen kabuk sayfası yalnız `renderPanel`/`renderScripted`'ten ve yanındaki
     krom sayfaya verilen view'un `PanelChrome`'u; ≥ 20 uyumlu çağrı · W05, W06.
  6. `TestPanelBrand_TheThemeHrefIsTheThemeRoute` · beş renk, `httpx.NewRouter(nil, nil,
     NewBrandTheme())` · `ThemeOf(c).Href()` = `/brand/theme/<HEX>.css`, 200 ve gövde
     `brand.ThemeCSS(c)`; sıfır `Theme` bağlanmaz · W21.
  7. `TestPanelBrand_TheLogoSrcIsTheLogoRoute` · tap + panel + logo rotaları, A işletmesi ·
     `adminLogoHref` rotanın yolu; kromun `<img src>`'i aynı çerezle 200 `image/png` + A'nın
     baytları · W29.
  8. `TestPanelBrand_TheSignInFamilyStaysTaptime` · iki işletmeli giriş → picker, panel problem
     sayfası, tam markalı okuyucu · yürüyüş boyunca okuyucu 0 kez; picker ve problem sayfası:
     Wordmark, marka izi yok, `adminCSP` literal'i; giriş sayfası: politikada img-src yok
     (Wordmark ve sıfırlama ailesi ölçülmez) · W26.
  9. `TestPanelBrand_TheReaderIsRequired` · nil ve typed-nil okuyucu · kurucu hata döner · W30.
  10. `TestPanelLogoOf_KeepsTheProportionsInsideTheSlot` · 11 listeli boyut, 4 ret, 1–512 × 1–512
      taraması (262 144) · kutu 192 × 32'ye sığar, saklanandan büyük değil, bir kenar yuvada ya da
      saklandığı gibi, öteki en yakın piksele yuvarlanmış oran (ya da 1) · W18, W19.
  11. `TestBrandReadDB_PanelBrandIsOneStatementForItsOwnBusiness` · dev Postgres, iki işletme,
      ayrı ad/accent/logo · her okuma kendi ad, accent ve logosu; 1 transaction, 1 ifade · W17.
  12. `TestBrandReadDB_PanelBrandUnbrandedShapesAreTheZeroValue` · satırsız, bütün alanları NULL,
      geçen accent, kolona doğrudan yazılmış 808080, 808080 + logo · sıfır değer / ad yalnız
      markalıda / `AccentRefused` · W12, W14.
  13. `TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError` · tohum satırlar (ErrNoRows, okuma hatası, 14 yarım logo, 5 bozuk accent, geçen/reddedilen
      accent, reddedilen + logo) · hata olanlar hata, sıfır değer olanlar sıfır, ad yalnız
      markalıda · W12, W13, W14.
  14. `TestBrandRead_PanelBrandNeedsATenantAndReportsAFailingDatabase` · nil tenant, reddeden DB ·
      hata, 0 transaction; DB hatası sıfır değer değil · W31.
  15. `TestBrandRead_TheBeltSeesThePanelRead` · paketin türetilmiş store çağrıları ·
      `GetTenantPanelBrand` içinde (öncül: kuşak bu paketten türer; kıran değişiklik çağrının paketten
      çıkması — koşulmadı, WL-6'nın `TestBrandRead_TheBeltSeesBothReads` emsali) · —.
  16. `TestStaffQueries_CarryAnExplicitTenantPredicate` (var olan kuşak) · `GetTenantPanelBrand`'in
      gövdesi · özne `tenant_branding`'in `tenant_id`'si üst düzey bağlaçta parametreye bağlı ·
      W15, W16.
  17. `TestTenantBranding_EveryStatementNamesTheTenant` (sayı 7 → 8) ·
      `internal/store/branding.sql.go` sabitleri · her SELECT/UPDATE'in son WHERE'i nitelemesiz
      `tenant_id = $N` · W15.
  18. `TestTenantBranding_PerPageReadsDoNotSelectTheLogo` · panel okumasının seçim listesi ·
      tam `name,accent,logo_sha256,logo_mime,logo_width,logo_height`, `logo` belirteci yok · W32.
  19. `TestStoreSurface_IsTheOneRecorded` (`cmd/tappa`, envanter + 121 → 122) · üretilmiş
      `*Queries` yüzeyi · envanterle birebir · W32.
  20. `TestCompiledCSS_BrandVariablesOnlyInTheirSlots`, `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`
      (WL-5'in; `themeSlots` + golden'a şerit) · derlenmiş `app.css` · şerit yalnız
      `background-color`'da `--brand-accent`; golden beş yer · W10, W11, W33.
  21. `TestThemeSlotScan_RefusesEachShapeItExistsFor`, `TestThemeBrandScan_RefusesEachShapeItExistsFor`
      (negatif kontroller; dört + iki yeni şekil) · sevk edilen şekil şeritle geçer; şeridin
      accent'siz, `color`'da, etiket ya da kenarla okuması ve şerit kuralının `.btn--primary`'ye
      taşınması ya da seçicisinin ona genişlemesi raporlanır · W33.
- **PART II — pinler ve yakaladıkları:** PART I'in parantez içi mutasyonları — aşağıdaki
  tabloda, kırmızıya döndükleri testle.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusudur; tamlık iddiası yoktur.

### Mutasyon tablosu

**1. turda 33 mutasyon, 33'ü kırmızı; düzeltme turunda W01–W33 (W25 hariç — `alt=""` artık
sevk edilen şekil, yerini R02 aldı) son koda karşı yeniden ve R01–R06 ilk kez koşuldu: 38
mutasyon, 38'i kırmızı, derleme hatası 0, hepsi sha256 ile geri yazıldı.**

| ID | Yer | Mutasyon | Kırmızıya dönen |
|---|---|---|---|
| W01 | `panelbrand.go` | okuma hatasında ERROR logu silinir (sessiz varsayılan) | `TestPanelBrand_UnbrandedSectionsAreTheWordmarkChrome` |
| W02 | `adminlogin.go` `renderPanel` | `adminCSPFor(false)` | `TestPageImages_ImgSrcIsNamedOnlyByAPageThatDrawsAnImage`, `TestPanelBrand_ABrandedBusinessGetsItsHeaderOnEverySection` |
| W03 | `adminlogin.go` `renderPanel` | `adminCSPFor(true)` | `TestPageImages_…`, `…ABrandedBusiness…`, `…UnbrandedSections…` |
| W04 | `adminlogin.go` `renderScripted` | img-src `false` | `TestPageImages_…`, `…ABrandedBusiness…` |
| W05 | `review.go` | kabuk sayfası `a.render` ile | `TestPageImages_…`, `…ABrandedBusiness…`, `TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy` |
| W06 | `review.go` | `renderPanel(…, pages.PanelChrome{}, …)` (başka krom) | `TestPageImages_…`, `…ABrandedBusiness…`, `TestPanelRenders_…` |
| W07 | `admin.templ` | şerit koşulsuz (`if true`) | `…ABrandedBusiness…` |
| W08 | `base.templ` | tema bağlantısı `app.css`'ten önce | `…ABrandedBusiness…` |
| W09 | `base.templ` | tema bağlantısı koşulsuz (boş href) | `…ABrandedBusiness…`, `TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`, `…UnbrandedSections…` |
| W10 | `input.css` | şerit accent'i `color`'da da okur | `TestCompiledCSS_BrandNamesOccurOnlyInTheGolden`, `TestCompiledCSS_BrandVariablesOnlyInTheirSlots` |
| W11 | `input.css` | `.btn--primary` accent zemini (K4 ihlali) | aynı ikisi |
| W12 | `brandread.go` `accentOf` | `Check` atlanır | `TestBrandReadDB_PanelBrandUnbrandedShapesAreTheZeroValue`, `TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError` |
| W13 | `brandread.go` `panelBrandOf` | yarım logo hatası yutulur ("logo yok") | `TestBrandRead_PanelBrandOfKeepsEveryNonBrandAnError` |
| W14 | `brandread.go` | ad markasıza da verilir | `…UnbrandedShapes…`, `…PanelBrandOfKeeps…` |
| W15 | `branding.sql` + sqlc | `WHERE tenant_id = @tenant_id` silinir (yalnız `tenants.id`) | `TestStaffQueries_CarryAnExplicitTenantPredicate`, `TestTenantBranding_EveryStatementNamesTheTenant` |
| W16 | `branding.sql` + sqlc | `… OR true` | `TestStaffQueries_CarryAnExplicitTenantPredicate` |
| W17 | `brandread.go` | aynı tx'te ikinci ifade (`GetTenantBrand`) | `TestBrandReadDB_PanelBrandIsOneStatementForItsOwnBusiness` |
| W18 | `panelbrandview.go` | küçük logo büyütülür | `TestPanelLogoOf_KeepsTheProportionsInsideTheSlot` |
| W19 | `panelbrandview.go` | yuvarlama yerine taban | `TestPanelLogoOf_…` |
| W20 | `review.go` `chrome()` | marka iki kez okunur | `…ABrandedBusiness…`, `…UnbrandedSections…` |
| W21 | `layout/theme.go` | href küçük harf hex | `…ABrandedBusiness…`, `TestPanelBrand_TheThemeHrefIsTheThemeRoute` |
| W22 | `panelbrand.go` | digest denetimi kapatılır | `…UnbrandedSections…` |
| W23 | `panelbrand.go` | reddedilen accent WARN'ı kapatılır | `…ABrandedBusiness…`, `…UnbrandedSections…` |
| W24 | `admin.templ` | Wordmark markalıda da çizilir | `…ABrandedBusiness…` |
| W25 | `admin.templ` | *(1. tur: `alt=""` — artık sevk edilen şekil; düşürüldü, bkz. R02)* | — |
| W26 | `adminlogin.go` `ChoosePage` | picker markayı okur | `TestPanelBrand_TheSignInFamilyStaysTaptime` |
| W27 | `panelbrand.go` | reddedilen accent de tema alır | `…ABrandedBusiness…` |
| W28 | `base.templ` `Panel` | temayı düşürür (`Theme{}`) | `…ABrandedBusiness…` |
| W29 | `brandlogo.go` `adminLogoHref` | tap rotasının yolu | `…ABrandedBusiness…`, `TestPanelBrand_TheLogoSrcIsTheLogoRoute` |
| W30 | `adminlogin.go` | `isNil(brands)` reddi silinir | `TestPanelBrand_TheReaderIsRequired` |
| W31 | `brandread.go` | nil tenant reddi silinir | `TestBrandRead_PanelBrandNeedsATenantAndReportsAFailingDatabase` |
| W32 | `branding.sql` + sqlc | panel okuması `logo`'yu da seçer | `TestTenantBranding_PerPageReadsDoNotSelectTheLogo`, `TestStoreSurface_IsTheOneRecorded` |
| W33 | `theme_test.go` | `themeSlots`'tan `.panel-stripe` silinir | `TestCompiledCSS_BrandVariablesOnlyInTheirSlots`, `TestThemeSlotScan_RefusesEachShapeItExistsFor` |
| R01 | `admin.templ` | logonun `alt` özniteliği silinir (denetçinin M07'si) | `…ABrandedBusiness…` |
| R02 | `admin.templ` | `alt={ b.Name }` geri yazılır | `…ABrandedBusiness…` |
| R03 | `panelbrand.go` | WARN satırına `"business", b.Name` (denetçinin M14'ü) | `…ABrandedBusiness…` |
| R04 | `panelbrand.go` | WARN satırına `"accent", b.Accent.Hex()` | `…ABrandedBusiness…` |
| R05 | `panelbrand.go` | WARN satırına `"logo", b.Logo.SHA256` | `…ABrandedBusiness…` |
| R06 | `admin.templ` | adın paragrafına `text-saffron` (denetçinin M27'si) | `…ABrandedBusiness…` |

Koşu kapsamları: handler mutasyonları `-run 'TestPanelBrand_|TestPanelLogoOf_|TestPanelRenders_|TestPageImages_|TestPagePolicies_'`
(W30 yalnız `TestPanelBrand_TheReaderIsRequired`); domain `-run PanelBrand` (+ belt) .env'li; CSS
`internal/brand`'in derlenmiş-CSS testleri ve iki tarama kontrolü (her CSS mutasyonu `app.css`'i yeniden derler);
sorgu mutasyonları `make sqlc` + `internal/domain/tenant`, `internal/db`, `cmd/tappa`.

### Sayılı sınırlar (WL-8)

1. Golden yalnız listelediği 24 bileşen render'ını tutar; bölüm yanıtları (`time.Now()` içerir)
   golden'da değil — router düzeyinde dört markasız varyantın birbirine eşitliği ve markalı
   sayfanın, tema bağlantısı ve başlık geri alınınca markasız sayfaya eşitliği ölçülür.
2. Chrome ölçümü bir kez, pin değil; 390 genişlik headless'ta ölçülemedi (pencere ~500'ün
   altına inmiyor, görüntü kırpılır — WL-5 notundaki gibi).
3. `GetTenantPanelBrand`'in `tenants` tarafı pinsiz. `AND tenants.id = @tenant_id`'yi tek
   başına silmek eşdeğer bir mutanttır (join koşulu `tenants.id = tenant_branding.tenant_id`
   ve öznenin `tenant_id = @tenant_id`'si aynı satırı bağlar). Pinsiz olan, `tenants`
   tarafının join koşulunun bütünüdür: `JOIN tenants ON true` + yüklemin silinmesi
   (denetçinin M22'si) yeşil kaldı — sonucu yalnız `tenants`'ın RLS'i
   (`tenants_tenant_isolation`) tutar. İki kuşak da özneyi (`tenant_branding`) okur. Bu iki
   ayrı düzenleme ister, yani kazara sapma tehdit modelinin dışındadır; kod incelemesinin
   konusu.
4. `TestPanelRenders_AShellPageIsRenderedWithItsChromesPolicy` yalnız çağrıda adıyla geçen kabuk
   sayfasını görür; değişkende tutulup sonra render edilen bileşeni görmez.
5. Kırık logo (404, iptal) `alt=""` ile kutusunu korur — ölçüldü, kısa ve 112 karakterlik adla
   kutu dışında 0 px; normal yüklemede kutu öznitelik oranından ayrılır (HTML'in en-boy
   eşlemesi) — **bekleyen** hâl headless'ta ayrıca yakalanamadı (`--timeout` isteği iptal edip
   kırık hâle düşürüyor). Ölçüm Chrome 154'te, bir kez; pin değil.
6. DB testlerinin fikstürleri dev DB'de kalır (`tenant_branding`'de DELETE yok).
7. *(1. tur: kapandı.)* Panelde logonun `alt`'ı boş (karar 9); logo ekran okuyucuya bir şey
   söylemez, adı görünür metin taşır.

### 1. tur düzeltmeleri (2026-10-03; güvenlik denetimi ONAY, üçüncü göz ONAY, altı bloklamayan bulgu)

- **F1 — logonun `alt`'ı.** Bulgu: `alt` = ad iken kırık logo adı kutuya basıyor; 114
  karakterlik adda kutu 128 × 216, içerik +145 px (denetçi); "~5 px" yalnız 16 karakterlik
  fixture'ın sayısıydı; ad iki kez okunuyordu. Değişiklik: `panelBrandHeader`'da `alt=""`
  (orkestratör kararı); test `alt`'ın **var ve boş** olduğunu ister; karar 9, sınır 5 ve 7,
  Ölçümler metni. Ölçüm: kırık logo kutu dışında 0 px (18 ve 112 karakter); R01 (`alt` silindi)
  ve R02 (`alt`'a ad) kırmızı.
- **F2 — log yorumunun hükmü.** Bulgu: WARN satırına adı yazan mutant (M14) yeşildi. Değişiklik:
  PART I 3'ün ret varyantı logda adı (tamamı, ilk 8 karakter), reddedilen accent'in hex'ini ve
  digest'i (tamamı, ilk 8 hane) arar; fake ret varyantında reddedilen accent'i de taşır. Ölçüm:
  R03 (ad), R04 (accent), R05 (digest) kırmızı. `panelBrand`'in yorumu ERROR satırlarının yarısını
  "ölçülmedi" diye ayırır.
- **F3 — giriş sayfası.** Kabul 5, karar 10, PART I 8 ve testin yorumu testin ölçtüğüne
  daraltıldı (YALNIZ METİN).
- **F4 — sınır 3.** `tenants` tarafının join koşulu adlandırıldı; tek yüklem silmenin eşdeğer
  mutant olduğu yazıldı (YALNIZ METİN).
- **F5 — "+1 okuma".** PART I 2/3, karar 1, `review.go` `chrome()` yorumu ve `panelbrand.go`
  başlığı "okuyucuya istek başına bir çağrı + okuyucuda bir ifade"ya daraltıldı (YALNIZ METİN).
- **F6 — adın rengi.** Seçim: pinle (ucuz). Ad ve co-brand paragrafları tam sınıf listeleriyle
  doğrulanır; R06 (adın paragrafına `text-saffron`) kırmızı.

### Devirler

- **WL-7:** Account önizlemesi `AdminAccount` içinde, `renderPanel`'den geçer; politika bugün
  yalnız kromun `DrawsLogo`'suna bakar. Önizleme `<img>`'ini ayrı bir okumayla çizerse (krom
  okuması düşüp önizlemeninki başarılı olursa) img-src eksik kalır: önizleme kromun
  `PanelChrome.Brand`'ini kullanmalı ya da `renderPanel`'e kendi `<img>`'ini OR'lamalı (pin ve
  img-src derlemi birlikte güncellenir). `NewAdminAuth`'a yazma tarafı `brands`'ten ayrı alan
  olarak girer. Bayt (ADR 0024 WL-6 sınır 12): kabuk her bölümde logoyu çizer — sayfa başına
  soğuk +1, sıcak 0.
- **WL-9:** tap ekranının tema bağlantısı `layout.Theme` + `documentHead`'in `theme`
  parametresiyle verilebilir; birleştirmede tek mekanizma kalmalı. Okuma tarafı kapısı
  `accentOf` yeniden kullanılabilir. *(WL-9 birleştirmesi, 2026-10-03: tek mekanizma kaldı —
  tap kabuğu temayı `documentHead`'in `theme` parametresiyle yazar; okuma tarafında WL-9'un
  `pageBrandOf`'u accent için `accentOf`'u çağırır — kapının bugün reddettiği accent tap ekranında da
  yalnız accent'i düşürür, logo kalır, WARN.)*
- **WL-10:** sınırlar 1–7; `base.templ`, `renderPanel` ve `GetTenantPanelBrand` diff'i.
- **WL-12:** skill *"Tenant slotları"*: panel satırı ölçüldü (32 px yuva, ≤ 192 px; `.panel-stripe`
  yalnız `--brand-accent`/`background-color`; yalnız accent'li işletmede de ad + co-brand);
  skill'in *"alt = tenant adı"* cümlesi yüzeye göre yazılmalı: tap ekranında ad, panel
  başlığında boş (karar 9).
