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
uygulanmaz (→ **WL-5**: *"tenant accent'i yalnız marka slotlarında"* testi — adı WL-5'te
konur; mutasyon: `.stamp`'e accent zemini → kırmızı):

1. Beş kaşe damgası (APPROVED / FLAGGED / REJECTED / IGNORED-RECORDED / TRAINING) ve
   durum→renk eşlemesi (`input.css:180-189`; `TestCompiledCSS_StampWordIsInk`).
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
- **PART I:** bugün rota yok (ölçüldü). WL-5'te ölçülecek: rota kurucusunun imzası bir havuz
  ya da sorgu arayüzü almaz; 200 gövdesi `^:root\{--brand-accent:\d{1,3} \d{1,3} \d{1,3};
  --brand-on-accent:…;--brand-edge:…\}$` biçimine birebir uyar; kanonik olmayan, küçük harfli,
  geçersiz ve red bandındaki hex 404 alır.
- **PART II:** WL-5'in kurucu imzası testi (havuz parametresi yok) · gövde biçimi testi ·
  200/404 matris testi. Yakaladıkları: kurucuya havuz eklenmesi; gövdeye üç özellik dışında bir
  şey girmesi; `Check`'ten geçmeyen bir hex'in 200 alması.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia B — marka ayarlamamış tenant'ın sayfası bugünküyle aynıdır.**
- **PART I:** WL-9'da ölçülecek: marka satırı olmayan bir tenant'ın tap ve sonuç ekranının HTML
  gövdesi ve CSP başlığı, değişiklik öncesi golden dosyayla bayt-aynı. WL-5'te bir kez ölçülecek
  (CDP, pin değil): tap düğmesinin computed zemini `rgb(31, 92, 65)`, metni `rgb(255, 253, 244)`.
- **PART II:** WL-9'un golden testi — **WL-9 kartında adıyla ve sayısıyla listelenen**
  fikstürler (tap ekranı; sonuç ekranının hüküm × yön × iş türü × practice varyantlarından
  seçilenler; aktivasyon ailesi), her biri HTML + CSP · WL-5'in derlenmiş-CSS testi —
  `app.css`'teki `:root` bildiriminde `--brand-accent` ve `--brand-on-accent` varsayılanlarını
  okur (`TestCompiledCSS_StampWordIsInk` emsali; `app.css` yoksa o da atlanır). Yakaladıkları:
  markasız tenant'ın **golden fikstürü olan** render'larının HTML'inde ya da CSP'sinde bir bayt
  farkı; `:root` varsayılanlarının tappa-green / paper'dan sapması (derlenmiş-CSS testi).
  Fikstürü olmayan bir varyant bu testin dışındadır.
- **PART III:** Listede olmayan her biçim kod incelemesinin konusu — tamlık iddiası yok.

**İddia C — accent §6'nın sayılı öğelerine uygulanmaz.**
- **PART I:** WL-5'te ölçülecek: derlenmiş CSS'te marka değişkenlerini okuyan kurallar §2'nin
  slot sınıflarındadır ve §3'ün özellik kuralına uyar (`--brand-accent` yalnız
  `background-color`'da); `.stamp`'e accent zemini eklemek ve `.tap-button`'ın `color`'ına
  `--brand-accent` yazmak (iki mutasyon) testi kırmızıya çevirir.
  WL-9'da ölçülecek: markalı (accent'li) bir tenant'ın sonuç sayfasında tema `<link>`'i 0 —
  D-C'nin *"sonuç ekranında accent yok"* yarısı.
- **PART II:** WL-5'in slot testi (adı WL-5'te) · `TestCompiledCSS_StampWordIsInk` — derlenmiş
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
- **WL-9'un 16 px bütçesi ile K-2b'nin logo yuvası** (§5'in aritmetiği: 16 px'te yuva
  ≤ `29 − g`) — orkestratörün piksel kararı, WL-9'da.
- Tur / practice ekranlarında logo (faz 2) ve o ekranlardaki altı `.tap-button`'ın accent alıp
  almayacağı — §9 sorusudur, faz 2 açılınca sorulur.
- E-postada *"X via Taptime"* gönderen adı (yalnız VIES-doğrulanmış tenant koşuluyla, WL-11) ve
  logo (faz 2, CID, ≤32 KiB varyant; uzak URL elendi — izleme pikseli).
- Accent'in durum renklerine yakınlık kuralı (sınır 2).
- Logo yuvasının kesin boyutları — skill taslağı aritmetikle öneri yazar; WL-8/WL-9 ölçer.

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
