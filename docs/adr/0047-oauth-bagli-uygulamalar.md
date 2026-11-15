# ADR 0047 — OAuth 2.0 "Connected apps": Taptime yetkilendirme sunucusu olur, Malta Calculator ilk istemci

- **Durum:** **Kabul edildi (2026-11-14).** Kullanıcı ADR'nin genelini onayladı ve §0'daki kararları verdi
  (OA-5 (b) 30 sn tolerans, OA-3 (a) grant başına webhook ayrı iş, davet kapısı "API açık veya onaylı
  uygulamaya canlı grant", sandbox yerel derleme); üç denetim turunun bulguları işlendi. Kod, migration, CLAUDE.md değişikliği **yok** — CLAUDE.md §10: güvenlik
  sınırı değişikliği.
  **Revizyon 1 (2026-11-14):** kullanıcı kararları işlendi (§0); bağımsız denetimin bulguları kapatıldı —
  YÜKSEK-1 davet kuyruğunun K-22 kapısı (§11.3), ORTA-1 holding eşlerinin verisi bütün uçlardan (§8.5),
  ORTA-2 oran sınırının gerçek bedeli (§8.7), ORTA-3 addendum ve RFC uyumu (§5, §18), DÜŞÜK-1…8 (§4, §10,
  §12, §14, §3), CSP ölçümü ve B planı (§4.6, §17).
  **Revizyon 2 (2026-11-14, 2. tur denetimin RED'i):** ORTA-A — adres kovası T90'ın `Reserve`/`Keep`/`Refund`
  kalıbıyla, kesin sınır (§8.1, §8.7, §5.1); refresh toleransı grant'ın bütün canlı tokenlarını iptal eder,
  `parent_id` kalktı (§5.3, §14.2); ömürler tek zaman kaynağından (`now()`, §14.2); operatörün K-22 kapanışı
  grant'tan üstün, davet kapısı `employees:write`'a bağlı, istemci kapanışı (§11.3, §11.4, §12); §18.2'ye beş
  madde; rota sınıfları, onay GET zinciri, SQL'de dönüş URI kuralı, md.7 (i) sapması (§4.2, §10.2, §14.3, §17).
- **Tarih:** 2026-11-14 · **Issue:** [atknatk/tappa#37](https://github.com/atknatk/tappa/issues/37) (gövde +
  *"Addendum: what the Malta Calculator client already expects"* yorumu) · **Bağlı:** #36
  (`GET /v1/timesheets`, ADR 0046 — paralel dalda; bu ADR o uca yalnız adıyla bağlanır)
- **Dal:** `api-oauth` (taban `origin/main` `c89c3b5b`)
- **Ayrılmış numaralar:** ADR 0047. Migration numarası **verilmez**: `origin/main`'in sonu `00103`, M16 dalı
  `00104`'ten başlar (ADR 0045, dal `plan/m16-mobil`); bu işin migration'ları **`main`'e birleştirme anında
  sıradaki** numaraları alır (§14.6).
- **Karar veren:** kullanıcı (ürün sahibi) — §0. *"Orkestratör kararı (öneri), kullanıcıya raporlandı"*
  işaretli maddeler kullanıcının ayrıca sormadığı, gerekçesi yazılı tasarım kararlarıdır.
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) md.7 (çözümleyiciler) ·
  [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) / [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md)
  (operatör, `op_*`) · [ADR 0022](0022-islemsel-eposta.md) (davet) · [ADR 0025](0025-tenant-askiya-alma.md)
  (askı) · [ADR 0027](0027-holding-ve-capraz-tenant-dokunusu.md) (holding eşleri) ·
  [ADR 0028](0028-dis-api-anahtar-akis-webhook.md) (API, anahtar, K-22, webhook, davet kuyruğu, sandbox) ·
  [ADR 0031](0031-tek-kimlik-cok-isletme.md) (panel kimliği, üyelik, geçiş, form bağı) ·
  [ADR 0035](0035-panel-oturumu-sunucuda-biter.md) (panel oturumu 12 sa) · CLAUDE.md §4.1–§4.7, §6, §7, §10
- **Etkilenecek (uygulanınca):** yeni goose migration'ları (§14) · `internal/apikey` (Bearer zincirinin
  ikinci kimlik türü) · yeni paket `internal/oauth` (token biçimi, HMAC, PKCE, kod/token işleri) ·
  `internal/handler/api` (kapsam kontrolü, eş süzgeci, `GET /v1/business`, `openapi.yaml`) · yeni yüzey
  `/oauth/*` (ana host) · `internal/handler` (onay sayfası, *"Connected apps"*, giriş/geçiş dönüşü) ·
  `internal/handler/operator` (uygulama kaydı) · `internal/invite/queue.go` (kuyruk kapısı) ·
  `internal/httpx/ratelimit.go` kullanımı · `internal/config` (`TAPPA_OAUTH_HMAC_KEY`) ·
  `internal/db/resolve.go` (üç çözümleyici) · `db/queries/punchfeed.sql`, rehber sorgusu ·
  `scripts/secretscan.sh` (R7d önekleri) · `scripts/redline-check.sh` (R5 muafiyeti) ·
  `deploy/k8s/40-ingress.yaml` (`/oauth` log'suz yol) · `docs/api/integration-guide.md` + `.tr.md` ·
  CLAUDE.md §3, §6, §7 · ADR 0002 / 0021 / 0025 / 0028 notları.
- **Kelimeler:** **okundu** = dosyadan okundu, çalıştırılmadı. Bu ADR'de **çalıştırılarak ölçülen yoktur**
  (DB yok, test koşulmadı); *"ölçüldü"* yalnız `grep` sayımları ve dosya okumaları içindir ve öyle yazılır.
  Tarayıcı davranışına dayanan her iddia **"doğrulanmadı"** diye işaretlidir. Atıflar (`yol:satır`)
  `c89c3b5b`'ye göredir.

**Atıf kuralı.** *"Addendum"* = issue #37'nin Malta Calculator yorumu (satırları §18.1'de A1–A8 diye
numaralı); *"issue"* = #37'nin gövdesi. Kırmızı çizgiler *"CLAUDE.md §4.x"*; çıplak *"§N"* bu ADR'nin
bölümüdür. RFC'ler: 6749 (OAuth 2.0), 6750 (Bearer), 7009 (revoke), 7636 (PKCE), 9207 (`iss`), 9700
(OAuth 2.0 Security BCP).

---

## 0. Kullanıcı kararları (2026-11-14)

| # | Soru | Karar | İşlendiği yer |
|---|---|---|---|
| — | ADR'nin geneli | **Onaylandı** (durum *"Önerildi"* kalır; kabul notu orkestratörün commit'inde) | — |
| **OA-1** | Bağlanmak API'yi açar mı | **(a)** onaylı uygulamaya bağlanmak operatör adımı istemez; grant kendi kapısıdır. Son biçimi YÜKSEK-1 ile: K-22'nin her okuyucusu tek tek ele alınır, davet kuyruğu kapısı *"K-22 açık **veya** onaylı istemcinin canlı grant'ı var"* olur | §11.1, §11.3 |
| **OA-2** | `companyCode` | **(a)** `tenants.api_code`, yoksa `null` | §8.4 |
| **OA-3** | Grant başına webhook | **(a)** ayrı, sonraki iş; `webhooks:manage` bu teslimde **kayıtta verilemez** | §13 |
| **OA-4** | OAuth'ta holding eşleri | **(a)** eşlerin verisi OAuth tokenıyla **hiçbir uçtan** çıkmaz (ORTA-1 ile rehberden punch akışına genişledi) | §8.5 |
| **OA-5** | Refresh yeniden kullanımı | **(b)** 30 sn tolerans, sonrası grant iptali | §5.3 |
| **OA-6** | Kim onaylar | **(a)** yalnız aktif owner | §4.4 |
| **OA-7** | Kayıt arayüzü | **(a)** operatör ekranı + `op_*` | §10 |
| — | Sandbox | Şimdilik **canlı sandbox yok**; yerel sandbox derlemesiyle (`make build-sandbox`, `TAPPA_ENV=sandbox`) denenir. Yerel kayıt ve geliştirme callback'i önerisi §10.4'te | §8.6, §10.4 |

---

## Tek cümlede

İşletmenin **owner**'ı, operatörün kaydettiği bir uygulamaya (ilk: Malta Calculator) panel kimliğiyle
girip **tek bir işletme adına** ve **adı yazılı kapsamlarla** onay verir; uygulama Authorization Code +
PKCE (S256) + client secret ile **opak, hash'li, önekli** bir erişim tokenı (1 sa) ve dönen bir refresh
tokenı alır; `/v1` bu tokenı API anahtarının yanında kabul eder ama **yalnız kapsamının rotalarına ve yalnız
işletmenin kendi verisine** açar; token **tam olarak bir tenant'a** çözülür, RLS değişmez, tenant sınırını
aşan yeni bir yol açılmaz.

## Neden bir ADR

CLAUDE.md §10: yeni bir **güvenlik sınırı** (ikinci Bearer kimlik türü, tarayıcıdan geçen yetki kodu,
panel oturumunun bir dış uygulamaya yetki devri), yeni **şema** (üç tenant tablosu, bir tenant kapsamsız
kayıt tablosu, üç çözümleyici, beş `op_*`) ve **K-22'nin** (ADR 0028 §2 — *"API yalnız operatör açınca"*)
bir yetki türü için yeniden tanımı (§11).

---

## 1. Bağlam — bugün (okundu)

| Olgu | Kanıt |
|---|---|
| Üç host: ana host `taptime.mt` (panel, tap, landing), operatör `ops.taptime.mt`, API `api.taptime.mt` (yalnız `/v1`) | `deploy/k8s/05-config.yaml:24`, `:33`; `deploy/k8s/40-ingress.yaml`; `deploy/k8s/42-api-ingress.yaml:74-77`; `deploy/README.md:37-39` |
| API host'u iki yönlü kapılı: API host'unda `/v1` dışı her şey 404, başka host'ta `/v1` 404 | `internal/httpx/apihost.go:42-52`; `internal/handler/api/surface.go:213-221`; `internal/httpx/router.go:102-109` |
| Operatör host'unda `/operator`, `/operator/…` ve statik dışında her yol 404 (`/` → `/operator`'a 303) | `internal/httpx/operatorhost.go:111-121` |
| Panel çerezi `tappa_admin_session`, **`Path=/admin`**, `SameSite=Lax`, 12 sa | `internal/adminauth/cookie.go:28`, `:49`, `:176`; ADR 0035 |
| Oturumsuz panel isteği **sabit** `/admin/login`'e 303; giriş başarıda **sabit** `/admin`'e, geçiş başarıda **sabit** işlemler sayfasına 303 — dönüş adresi (*return-to*) **yok** | `internal/handler/adminlogin.go:963-968`, `:1494` (eski kol); `internal/handler/adminloginidentity.go:134` (kimlik kolu); `internal/handler/adminswitch.go:230` |
| Panel yazma zinciri: flood → Origin → kimlik → oturum bütçesi → **form bağı** → **askı halkası**; askıda açık kalanlar ayrı zincir (`protectWritingThroughSuspension`) | `adminlogin.go:919-928`, `:930-954` |
| Form bağı: her POST formunun action'ı oturum tenant'ının MAC'ini taşır; başka işletmeye geçmiş sekmenin formu reddedilir; **her yazma sabit bir panel yoluna 303 eder** (S14) | `internal/handler/formbinding.go:1-45` |
| **Çerez basan / iz yazan panel GET'leri** `mintGate`'in arkasında: `Sec-Fetch-Site` yalnız `same-origin`, `none` ya da yok ise geçer; `cross-site` ve `same-site` 403 (T14, T19) | `internal/handler/fetchsite.go:10-54`, `:83` |
| İşletme geçişi `POST /admin/switch` — yetki oturumdan + hedefteki üyelikten; kendi senkronizör token'ı; bayrağa (`TAPPA_ADMIN_IDENTITY`) bağlı | `internal/handler/adminswitch.go:7-56` |
| Kişinin üyelikleri rol ve askı bilgisiyle: `identity_memberships(p_session_hash)` → `(tenant_id, tenant_name, role, suspended, is_current)` | `db/migrations/00085_end_panel_sessions_after_twelve_hours.sql:1417-1418` |
| Roller kapalı küme: `owner`, `manager` | `db/migrations/00006_create_admin_users.sql:66` |
| API anahtarları **owner'a özel** (okuma dahil); bölüm *"Developers"*, `/admin/api-keys` | `internal/handler/apikeys.go:21`, `:67-69`; `web/templates/pages/adminview.go:413` |
| Panel CSP'si `form-action 'self'` ve `frame-ancestors 'none'` | `adminlogin.go:2192-2193` |
| Anahtar biçimi `tt_live_` / `tt_test_` + 43 base64url (32 bayt); bir dağıtım **tek** önek kabul eder; saklanan değer `TAPPA_API_KEY_HMAC_KEY` ile HMAC-SHA256, alan etiketi `tappa/apikey/v1\|` | `internal/apikey/token.go:39-62`, `:150-158` |
| Bearer zinciri sırası: başlık → biçim (DB'siz 401) → **adres kovası (her istek sayılır)** → çözümleyici → anahtar kovası → `last_used_at` → K-19; zincirden sonra `Authorization` **silinir** | `internal/handler/api/chain.go:10-26`, `:41-78` |
| Kova sabitleri: adres 1200/dk, anahtar 600/dk (bellekte, tek replika) | `surface.go:47-52` |
| **T90:** *"önce `Allowed`, sonra `Charge`"* iki ayrı kilitli adımdır, araya iş girince eşzamanlı N istek bütçeyi N kez geçer (ölçülüp kaldırıldı); yerine `Reserve` (şarj + karar tek kilitte) → iş → `Keep` (sayılır) ya da `Refund` (iade). `Allowed`'ın üretimde çağıranı **olamaz** — pinli | `docs/backlog.md:263`; `internal/httpx/ratelimit.go:195-253`; `cmd/tappa/limiterallowed_test.go:22` (`TestLimiterAllowed_IsCalledByNoProductionCode`) |
| Rota sınıfları: 1 panel yazması (`ProtectWriting`) · 2 adlı panel istisnası · 3 panel dışı · 4 metotsuz | `cmd/tappa/approuter_test.go:373-410` (`TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses`) |
| `RequireAdmin(codec, verifier, onUnauthenticated)` oturumsuz isteği verilen handler'a düşürür | `internal/httpx/adminidentity.go:128` |
| Operatörün K-22 kapanışı `api_enabled_at`'i NULL yapar, **`api_code`'u korur**; tenant'a `tenant.api_disabled` audit satırı; `api_code`'un tek yazarı `op_enable_tenant_api` | `db/migrations/00039_open_the_api_from_the_operator.sql:658-713`; `db/queries/operator.sql:231`; `00038:54-57` |
| Veritabanında sandbox'ı ayırt eden bir işaret yok (migration'larda `sandbox` yalnız `00100`'ün bir yorumunda); sanal UID kontrolü Go'da | `grep -in sandbox db/migrations/*.sql`; `internal/sandbox/plaque.go:47-92` |
| Çözümleyici *"taşı, uygulama"*: `resolve_api_key_by_hash` → `(tenant_id, key_id, revoked_at, api_enabled_at)`; karar Go'da, önbelleksiz | `db/migrations/00051_create_api_keys.sql:158-178`; `internal/apikey/auth.go:182-198` |
| K-22: API **yalnız operatör açınca** (`tenants.api_enabled_at` + `api_code`); gerekçe *"`/signup` herkese açık — aksi hâlde herkes sunucumuza istediği adrese istek attırabilirdi"* | ADR 0028:110, §2; `db/migrations/00038_add_api_access_to_tenants.sql:53-57` |
| `tenants.api_code` biçimi `^[A-Z][A-Z0-9]{1,15}$`, NULL olabilir; `api_enabled_at` doluysa zorunlu; yazarı yalnız `op_enable_tenant_api` | `00038:54-57`; `db/migrations/00039_open_the_api_from_the_operator.sql:595` |
| **K-22'nin bütün okuyucuları** (grep, ölçüldü): Go'da 6 yol, SQL'de 4 fonksiyon ailesi — tablo §11.3 | `grep -rn "api_enabled_at\|GetTenantAPIEnabledAt" internal db/queries cmd db/migrations` |
| Davet kuyruğu her daveti K-22'ye bağlar: kapalıysa **hiçbir şey basılmaz**, istek kalır, ömrü 72 sa | `internal/invite/queue.go:288-298`, `:82` |
| `invite_requests`'in birincil anahtarı `(tenant_id, employee_id)` — isteği kimin doğurduğu (anahtar/grant) yazılmaz | `db/migrations/00037_add_external_refs_and_the_invite_queue.sql:183-193` |
| `/v1` kimlikli rotaları (eksiksiz): `PUT /locations/{ref}`, `GET /employees`, `GET/PUT/DELETE /employees/{ref}`, `POST /employees/{ref}/resend-activation`, `GET /punches`, `GET /directory`; kimliksiz: `GET /openapi.yaml`, `/guide.md`, `/guide.tr.md`; sandbox derlemesinde ek `POST /sandbox/taps`, `GET /sandbox/openapi.json` | `surface.go:176-208`; `internal/sandbox/sandbox.go:182-193` |
| Rehber kendi çalışanlarını **ve** holding eşlerinin çalışanlarını döner (`holding_directory`) | `internal/handler/api/directory.go:23-27`; ADR 0028 §2 |
| Punch akışı: `scope=employer` kendi çalışanının eş plaketindeki punch'ını (**eşin lokasyonu**), `scope=location` kendi plaketindeki eş çalışanının punch'ını (**eşin çalışanı**) döner; tek tenant punch'ı iki kapsamda; süzgeç `@role = ANY (pe.roles)` | `internal/handler/api/punches.go:20-28` (employer `:23-24`, location `:25-26`); `db/queries/punchfeed.sql:47`, `:121`; `db/migrations/00048_create_the_punch_feed_tables.sql:167` (`roles` kümesi) |
| Hata kümesi kapalı (`errors.go:24-40`); 401'de `WWW-Authenticate: Bearer realm="taptime"` | `internal/handler/api/errors.go:139` |
| Pasifleştirme bütçesi audit'ten sayılır: `actor_id = @key_id`, `detail->>'via' = 'api'` | `db/queries/audit.sql:241-279` |
| `api_idempotency.key_id` → `api_keys(id, tenant_id)` FK'li | `db/migrations/00052_create_api_idempotency.sql:43-55` |
| Webhook: tenant başına **tek** uç nokta (`UNIQUE (tenant_id)`); tetikleyici teslim satırını **tek** uç noktaya yazar | `00048:195`; `db/migrations/00067_say_who_chose_a_punchs_direction.sql:289`, `:505`, `:519` |
| Anahtar işletmenindir, oluşturanın değil (T174) | ADR 0028:3870-3906 |
| `cmd/opadmin`: operatör hesabı için SQL yazan filtre (DB istemcisi değil, sunucu anahtarı tutmaz) | `cmd/opadmin/main.go:1-29` |
| R7d önekli sır taraması `tt_(live\|test\|whsec)_` — ön süzgeç, eşleşme ve gövde soyma | `scripts/secretscan.sh:486`, `:898`, `:903` |
| Süreç log'u URL değil **rota kalıbı** yazar (query'de sır taşınır); panel yolu ingress access log'u kapalı | `internal/httpx/requestlog.go:440-452`; `deploy/k8s/40-ingress.yaml:218-237` |
| Sandbox ikilisi `make build-sandbox` (`-tags sandbox`), yalnız `TAPPA_ENV=sandbox` ile başlar; `TAPPA_BASE_URL` varsayılanı `http://localhost:8080` | `Makefile:98-104`; `internal/config/config.go:328` |
| `/v1/punches`'ın `scope` parametresi (`employer`/`location`) — OAuth "scope"uyla **ad çakışması** | `docs/api/integration-guide.md:654-660` |

---

## 2. Karar özeti

| # | Konu | Karar | Bölüm |
|---|---|---|---|
| O-1 | Akış | Authorization Code + PKCE **S256 zorunlu** (`plain` ve eksik challenge red) + **gizli istemci zorunlu** (HTTP Basic). Public istemci, implicit, password, client_credentials, dinamik kayıt **yok** | §4, §5 |
| O-2 | Host | `/oauth/authorize`, `/oauth/token`, `/oauth/revoke` **ana host'ta**; `/v1` `api.taptime.mt`'de kalır. `TAPTIME_OAUTH_BASE_URL = https://taptime.mt` | §3 |
| O-3 | Çerez yolu | `/oauth/authorize` çerez okumaz, sorguyu koruyarak `/admin/oauth/authorize`'a **303** eder | §3, §4 |
| O-4 | Bekleyen istek | Doğrulanmış istek **her durumda** imzalı, kısa ömürlü `tappa_oauth_request` çerezine; onay sayfası onun özetini taşır; giriş / seçim / geçiş başarıda **tek sabit** devam yoluna 303 | §4.2 |
| O-5 | Onaylayan | Oturumun işletmesinde **aktif owner** (OA-6) | §4.4 |
| O-6 | Tokenlar | Opak, 32 bayt rastgele, önekli, `TAPPA_OAUTH_HMAC_KEY` ile HMAC'li saklanır; asla loglanmaz | §7 |
| O-7 | Ömürler | Kod **60 sn**, tek kullanım · erişim **1 sa** · refresh **90 gün boşta** (her kullanımda döner) · bekleyen istek 10 dk | §7.3 |
| O-8 | Grant | (tenant, uygulama) başına **tek canlı** grant; yeni onay eskisinin yerine geçer; grant **işletmenindir** | §5.2, §12 |
| O-9 | Refresh | Tek koşullu `UPDATE` + grant satır kilidi; yeniden kullanım **30 sn içinde** (ilk kullanımdan) öteki bütün canlı tokenları iptal edip yeni çift verir, sonra **grant iptali** (OA-5 (b)); token ucunda sunucu tavanı **D = 10 sn** | §5.3 |
| O-10 | `/v1` | Biçim DB'siz ayrılır; kapsam rota başına; eksikse 403 `insufficient_scope`; anahtarlar tam yetkili | §8 |
| O-11 | Holding eşleri | OAuth tokenıyla eşlerin verisi **hiçbir** uçtan çıkmaz (rehber + punch akışı + geçmiş sorgusu) | §8.5 |
| O-12 | Oran | Token biçimli istekte adres kovası T90 kalıbıyla: `Reserve` → reddedilirse `Keep`, kabul edilirse `Refund`; başarılı istek grant + istemci kovasına | §8.7 |
| O-13 | Yeni uç | `GET /v1/business` → `{id, name, companyCode}`; her kimlik okur | §8.4 |
| O-14 | Kayıt | Tenant kapsamsız `oauth_clients`; yazarı dört yazma `op_*`, okuyucusu bir okuma `op_*`; operatör ekranı | §10 |
| O-15 | Etkinleştirme | Grant hiç açılmamış K-22'yi istemez; **operatörün K-22 kapanışı grant'tan üstün** (`/v1` 403, davet kapalı); davet kuyruğu kapısı *"K-22 açık **veya** (operatörce kapatılmamış **ve** etkin istemcinin `employees:write`'lı canlı grant'ı)"* | §11 |
| O-16 | Webhook | Grant başına webhook kapsam dışı, izleyen iş; `webhooks:manage` kayda giremez | §13 |
| O-17 | Logo | İlk teslimde onay ekranında **ad + dönüş host'u**; logo sonraki iş | §10.3 |
| O-18 | Sandbox | Ayrı kayıt, ayrı önekler (`_test_`), ayrı HMAC anahtarı; yerel derlemede loopback callback; `POST /v1/sandbox/taps` OAuth'a kapalı | §7.1, §8.6, §10.4 |

---

## 3. Host ve uçlar

**Ölçü (okundu):** panel oturumu yalnız ana host'ta ve yalnız `/admin` altında vardır
(`cookie.go:49`); API host'u çerez taşımaz ve `/v1` dışını 404'ler (`apihost.go:42-52`). Onay ekranı panel
kimliğini istediği için **yetkilendirme ucu ana host'ta olmak zorundadır**. Addendum tek bir
`TAPTIME_OAUTH_BASE_URL` varsayar (A1); üç `/oauth/*` ucu bu yüzden aynı host'ta, ana host'tadır.

| Uç | Host | Çerez | Yüzey |
|---|---|---|---|
| `GET /oauth/authorize` | `taptime.mt` | **okumaz, yazmaz** | 303 → `/admin/oauth/authorize?<aynı sorgu>`; başka hiçbir şey yapmaz |
| `GET /admin/oauth/authorize` | `taptime.mt` | panel çerezi okunur; `tappa_oauth_request` **yazılır** | doğrulama + onay sayfası (§4) |
| `GET /admin/oauth/authorize/continue` | `taptime.mt` | panel + `tappa_oauth_request` | giriş/geçiş sonrası dönüş (§4.2) |
| `POST /admin/oauth/authorize` (`decision=allow`) | `taptime.mt` | panel + `tappa_oauth_request` | onay — `ProtectWriting` (askı halkalı) |
| `POST /admin/oauth/authorize/deny` | `taptime.mt` | panel + `tappa_oauth_request` | ret — `protectWritingThroughSuspension` (§4.5) |
| `POST /oauth/token` | `taptime.mt` | **okumaz, yazmaz** | sunucudan sunucuya; panel zincirinin **dışında**, `internal/handler/api` gibi ayrı yüzey |
| `POST /oauth/revoke` | `taptime.mt` | **okumaz, yazmaz** | aynı yüzey |
| `/v1/*` | `api.taptime.mt` | yok | bugünkü yüzey + kapsam + eş süzgeci (§8) |

- **Neden token ucu API host'unda değil:** addendum'un tek taban adresi (A1); token ucu tarayıcıya hiç
  dokunmaz, çerezsiz ve CSRF'siz bir yüzeydir — ana host'ta durması API host'unun *"yalnız `/v1`"*
  değişmezini bozmaz.
- **Başka host'larda `/oauth`:** API host'u bugünkü kapıyla 404'ler (`apihost.go:45` — `/oauth` `/v1`
  değildir); operatör host'u da 404'ler — `operatorHostOnly`'nin `switch`'i yalnız `/operator`, `/operator/…`,
  statik önek ve `GET /` için ayrı dal taşır, kalan her yol `default` dalında `http.NotFound`'dur
  (`internal/httpx/operatorhost.go:111-121`, okundu). Uygulama turunda ikisi pinlenir (§17).
- **Malta Calculator'ın iki taban adresi olur:** `TAPTIME_OAUTH_BASE_URL = https://taptime.mt` ve `/v1` için
  bugünkü API adresi (`https://api.taptime.mt`; rehber §1.3 — `docs/api/integration-guide.md:68-73`).
  Addendum `/v1` tabanını söylemiyor; bu bir **netleştirme**dir (§18).
- **Ingress:** `/oauth` yolu panel gibi **access log'suz** Ingress'e alınır (`40-ingress.yaml:218-237`'nin
  `/admin` kalıbı). `/oauth/authorize`'ın sorgusu sır taşımaz, ama istemcinin `state`'i bizim log'umuzda
  durmamalıdır.
- **Discovery (RFC 8414) yok** — tek istemci, sabit adresler; sonraki iş.

## 4. Yetkilendirme isteği ve onay ekranı

### 4.1 Parametre doğrulaması (RFC 6749 §4.1.2.1 — normatif sıra)

`GET /admin/oauth/authorize`; her parametre **en çok bir kez** (tekrar: istemci doğrulamasından önceyse
yönlendirmesiz hata, sonrasıysa `invalid_request`).

| # | Parametre | Kural | Hata |
|---|---|---|---|
| V1 | `client_id` | `app_` + UUID; kayıtlı; `disabled_at IS NULL` | **yönlendirme YOK** — Taptime'ın hata sayfası, 400 |
| V2 | `redirect_uri` | **zorunlu**; istemcinin kayıtlı listesindeki bir değerle **bayt bayt** aynı (normalleştirme yok: harf, sondaki `/`, port, `www.`) ; prod derlemesinde ayrıca `https://` ile başlamalı (§10.4) | **yönlendirme YOK**, aynı sayfa |
| V3 | `response_type` | tam olarak `code` | `unsupported_response_type` (kayıtlı adrese) |
| V4 | `code_challenge_method` | tam olarak `S256` (eksik → `plain` varsayılanı → red) | `invalid_request` |
| V5 | `code_challenge` | `^[A-Za-z0-9_-]{43}$` (SHA-256'nın base64url'i) | `invalid_request` |
| V6 | `scope` | boşlukla ayrık, boş değil, her değer kapalı kümede, küme ⊆ istemcinin izinli kümesi; tekrar eden değer tek sayılır, sıra önemsiz | `invalid_scope` |
| V7 | `state` | yoksa kabul (RFC); varsa ≤ 1024 bayt, RFC 6749 Ek A VSCHAR (`%x20-7E`) | `invalid_request` |

V3–V7'nin hataları **kayıtlı** `redirect_uri`'ye `error=…&state=…&iss=https://taptime.mt` ile 303 eder.
`state` aynen döner (URL kodlamasıyla); hiçbir yerde saklanmaz ve loglanmaz. Yanıta RFC 9207 `iss`
eklenir (§18).

### 4.2 Bekleyen istek çerezi ve oturum akışı

**Çerez her durumda yazılır.** V1–V7'den geçen her istek — oturum olsun olmasın — imzalı
`tappa_oauth_request` çerezine yazılır: `HttpOnly`, `Secure`, `SameSite=Lax`, `Path=/admin`, `Max-Age=600`;
içerik `client_id`, `redirect_uri`, `scope` (kanonik sıralı), `state`, `code_challenge`, oluşturma anı;
imza HMAC (`TAPPA_OAUTH_HMAC_KEY`, alan etiketi `tappa/oauth/request/v1|`). İçerik sır değildir (challenge
bir hash'tir); imza yalnız kurcalamayı keser. **Tek bekleyen istek** vardır: yeni bir yetkilendirme isteği
öncekinin yerine yazar.

**Onay sayfası isteği değil çerezi uygular.** Onay ve ret formları istek parametrelerini taşımaz; yalnız
`decision`, form bağı (`tb`) ve sayfanın **çizdiği isteğin özeti** `rd` = HMAC(`tappa/oauth/request-digest/v1|`
‖ çerez içeriği) taşır. POST, çerezin özeti `rd`'ye eşit değilse reddeder (*"This request changed — start
again from the app"*). Gerekçe: A uygulamasının onay sayfası açıkken başka bir sekmede (ya da bir
saldırganın tetiklediği gezinmeyle) B için bir yetkilendirme isteği çerezi ezerse, A'nın sayfasındaki
*"Allow"* **B'yi onaylamaz**.

| Durum | Akış |
|---|---|
| Oturum var, owner, işletme askıda değil | çerez yazılır → onay sayfası (§4.3–§4.5) |
| Oturum var, manager | çerez yazılır → *"Connecting apps is the owner's"* + işletme listesi (§4.3) + *"Back to <App>"* (ret POST'u) |
| Oturum var, işletme askıda | çerez yazılır → *"This business is suspended"* + işletme listesi + *"Back to <App>"* (ret POST'u) |
| Oturum yok | çerez yazılır → `/admin/login`'e 303 (bugünkü `requireLogin`); giriş (`Login`, `completePersonLogin`), seçim (`Choose`) ve geçiş (`/admin/switch`) **başarıda** geçerli bir çerez görürse `/admin` / işlemler sayfası yerine **tek sabit yol** `/admin/oauth/authorize/continue`'ya 303 eder |
| `continue` | çerezi doğrular (imza, yaş ≤ 10 dk) ve **V1–V7'yi yeniden koşar** (istemci hâlâ etkin, adres hâlâ kayıtlı); onay sayfasını çizer |

Dönüş hedefi bir **kapalı küme**dir (tek yol), istekten okunmaz — açık yönlendirme yoktur.

**Middleware zincirleri (normatif sıra).** Oturumsuz istekte de çerez yazılacağı için V1–V7
`requireAdmin`'in **önünde** koşar ve oturumsuz dal `RequireAdmin`'in `onUnauthenticated` handler'ıdır
(`adminidentity.go:128`) — `requireLogin`'in yerine.

| Rota | Zincir |
|---|---|
| `GET /oauth/authorize` | (panel zincirinin dışında) `floodGate("oauth_front")` → 303 `/admin/oauth/authorize?<sorgu>`; çerez okumaz/yazmaz, DB'ye gitmez |
| `GET /admin/oauth/authorize` | `floodGate("admin_oauth")` (panelin adres kalkanı, çözümleyiciden önce) → `oauthRequestGate` (V1–V7; V1–V2 hatası Taptime hata sayfası, V3–V7 hatası kayıtlı adrese 303; geçerse ayrıştırılmış istek bağlama) → `RequireAdmin(cookies, admins, onUnauthenticated = çerezi yaz + 303 /admin/login)` → `sessionGate` → handler (çerezi yaz, sayfayı çiz, §4.6 başlıkları). **`sameOriginGate` yok** (bir GET; gezinme `Origin` taşımaz) ve **`mintGate` yok** (aşağıdaki adlı istisna) |
| `GET /admin/oauth/authorize/continue` | `floodGate("admin_oauth")` → `RequireAdmin(…, onUnauthenticated = 303 /admin/login, çerez kalır)` → `sessionGate` → handler (çerezi doğrula, V1–V7'yi yeniden koş, çiz). `mintGate` **var**: bu yol yalnız bizim 303'ümüzle gelinir (`same-origin`) |
| `POST /admin/oauth/authorize` (onay) | `ProtectWriting` (flood → Origin → kimlik → oturum bütçesi → form bağı → askı halkası) — rota sınıfı **1** |
| `POST /admin/oauth/authorize/deny` (ret) | `protectWritingThroughSuspension` — rota sınıfı **2** (adlı istisna) |
| `POST /oauth/token`, `POST /oauth/revoke` | kendi yüzeyi: gövde sınırı → biçim → adres kovası (`Reserve`, §8.7) → istemci çözümleyicisi → istemci kovası — rota sınıfı **3** (panel dışı) |

Rota sınıfları `TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses`'ın (`cmd/tappa/approuter_test.go:373-410`)
kümelerine **adıyla** eklenir; Disconnect (`POST /admin/api-keys/apps/disconnect`, §12) sınıf **2**'dir.

**Onay POST'unda yeniden doğrulananların tam listesi** (hepsi aynı istekte, onay işleminden önce ya da
içinde):

1. çerezin imzası ve yaşı (≤ 600 sn); `rd` = çerezin özeti;
2. V1 `client_id` (kayıtlı, etkin — çözümleyiciden, önbelleksiz);
3. V2 `redirect_uri` hâlâ kayıtlı listede, bayt bayt (operatör arada değiştirmiş olabilir);
4. V3 `response_type = code` (çerez yalnız `code` isteklerinden doğar; yine de alan yazılır ve denetlenir);
5. V4 `code_challenge_method = S256`;
6. V5 `code_challenge` biçimi;
7. V6 `scope` ⊆ istemcinin **güncel** izinli kümesi, boş değil, bilinen değerler;
8. V7 `state` uzunluğu ve karakter kümesi;
9. panel zinciri: Origin kapısı + GATE-1, oturum, form bağı (`tb`), askı halkası (yalnız onayda);
10. onay işleminde: oturumun üyeliği **aktif owner** (rol işlemde yeniden okunur).

Çerez onay ya da ret POST'unda silinir.

**Adlandırılmış istisna — çapraz siteden gelen GET'in çerez yazması (DÜŞÜK-6).** Panelde çerez basan ya da
iz yazan GET'ler `mintGate`'in arkasındadır ve `Sec-Fetch-Site: cross-site`'ı reddeder (`fetchsite.go:10-54`,
T14/T19). `GET /admin/oauth/authorize` **zorunlu olarak** çapraz siteden gelir (istemcinin sitesinden
üst düzey gezinme; araya giren `/oauth/authorize` 303'ü `Sec-Fetch-Site`'ı değiştirmez — yönlendirme
zincirinin en çapraz değeri taşınır, doğrulanmadı) ve çerez yazar. Bu yüzden **`mintGate`'e bağlanmaz**;
bu, T14/T19 kuralına adıyla yazılmış bir istisnadır. Neden güvenli:

- yazılan tek şey imzalı bir **bekleyen istektir**: yetki vermez, kod basmaz, grant yaratmaz, audit satırı
  yazmaz, tenant verisine dokunmaz;
- istek V1–V2'den geçmiştir: yalnız operatörün kaydettiği bir istemci ve onun kayıtlı adresi;
- onay **ayrı, açık, aynı-origin bir POST**'tur (Origin kapısı + GATE-1 + form bağı + `rd`); çapraz site
  POST'u panel çerezini zaten taşımaz (`SameSite=Lax`);
- en kötü sonuç: kurbana *"X uygulaması bağlanmak istiyor"* sayfası gösterilir ve kurbanın açık bir onay
  sayfası varsa `rd` onu geçersiz kılar (yukarıda) — bu, `mintGate`'in koruduğu *"istenmemiş uyarı"* ile
  aynı sınıftadır ve OAuth'un tanımı gereği kaçınılmazdır.

### 4.3 Hangi işletme — çok işletmeli kimlik

- Onay **oturumun işletmesi** adına verilir (`httpx.AdminOf(r)`); tenant hiçbir zaman formdan ya da
  sorgudan okunmaz (ADR 0031 §3: *"form hangisini seçer, izin vermez"*).
- Sayfa işletmeyi **adıyla** yazar: *"Malta Calculator wants to connect to **Kebab Factory Ltd**"*, ve dönüş
  host'unu: *"You'll return to maltacalculator.com"*.
- Kişinin başka owner üyelikleri varsa (`identity_memberships` — `00085:1417-1418`; `role='owner'`,
  `suspended=false`, `is_current=false`), sayfa *"Not this business?"* altında onları listeler; her biri
  **bugünkü** `POST /admin/switch` formudur (kendi senkronizör token'ı, form bağı, üyeliğin hedefte yeniden
  doğrulanması — `adminswitch.go:7-40`). Geçiş başarıda §4.2'nin kuralıyla `continue`'ya döner. **Yeni
  tenant geçiş yolu açılmaz**; seçim mevcut geçiş mekanizmasının kendisidir.
- `TAPPA_ADMIN_IDENTITY` kapalıysa (geçiş yüzeyi yok) liste çizilmez; kişi çıkıp öbür işletmeyle girer.
- Form bağı (`formbinding.go`) onay POST'unu da bağlar: KF için çizilmiş onay sayfası, sekme KM'ye geçtikten
  sonra gönderilirse **reddedilir** — KM adına sessiz onay olmaz.

### 4.4 Kim onaylayabilir (OA-6 = (a))

**Yalnız aktif owner.** Gerekçe: API anahtarları owner'a özeldir, okuma dahil (`apikeys.go:21`, `:67-69`);
bir grant anahtarla aynı sınıftan bir yetkidir (işletmenin kayıtlarını dışarı açar, `employees:write` ile
pasifleştirebilir). Manager *"Connecting apps is the owner's"* görür ve *"Back to <App>"* ile
`error=access_denied` döner. Rol, onay POST'unun **kendi işleminde** yeniden okunur (anahtar alanının
`mayManage` kalıbı — `apikeys.go:22-24`).

### 4.5 Onay, ret, yanıt

- **Onay** — `POST /admin/oauth/authorize`, `ProtectWriting` zinciri (askıdaki işletmede askı halkası 403
  verir — yeni grant bir yazmadır, §11.2). §4.2'nin on maddesi geçerse `oauth_codes` satırı yazılır;
  303 → `redirect_uri?code=<kod>&state=<state>&iss=https://taptime.mt`.
- **Ret** — `POST /admin/oauth/authorize/deny`, **`protectWritingThroughSuspension`** zinciri (askı halkası
  **yok**; DÜŞÜK-1): ret **hiçbir satır yazmaz** (yalnız bekleyen istek çerezini siler), bir yazma değildir,
  bu yüzden askıdaki işletmede de istemciye `error=access_denied` ile döner. Ret POST'u da çerezi, `rd`'yi ve
  V1–V2'yi doğrular (yönlendirme hedefi kayıtlı adres olmalı); V3–V7 geçmezse yine `access_denied` döner
  (ret yolu bir hata yolu değildir). ADR 0025'in kapalı istisna listesine **adıyla** eklenir
  (`TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException`'ın türettiği küme).
- Onay **ya hep ya hiç**: kapsam onay ekranında daraltılamaz (v1 sadeliği).
- **Form bağının S14 değişmezine adlandırılmış istisna** (`formbinding.go:41-45`: *"her yazma sabit bir panel
  yoluna 303 eder"*): onay ve ret yanıtı **dış** bir adrese 303 eder. Hedef istekten değil **kayıttan**
  okunur (bayt bayt eşleşmiş kayıtlı URI); yalnız `code`, `state`, `iss`, `error` eklenir; form bağı
  parametresi (`tb`) URL'ye **taşınmaz**.

### 4.6 Başlıklar ve `form-action` (CSP)

- **A planı (birincil):** onay ve ret sayfalarının CSP'si panelinki (`adminlogin.go:2192-2193`) **artı**
  `form-action`'a **yalnız bu istemcinin, bu istekte eşleşmiş** `redirect_uri`'sinin origin'i (ör.
  `form-action 'self' https://maltacalculator.com`). Gerekçe: CSP `form-action` form gönderiminin
  **yönlendirme zincirine** de uygulanabilir; yalnız `'self'` ile 303 tarayıcıda bloklanabilir. Bu davranış
  **doğrulanmadı**; uygulama turunda Chrome, Firefox ve Safari'de **iki biçim** ölçülür: (i) yalnız
  `'self'`, (ii) A planı (§17). Origin listesi sayfa başına hesaplanır; başka bir istemcinin ya da başka
  bir kayıtlı adresin origin'i eklenmez.
- **B planı (A planı bir tarayıcıda bloklanırsa):** onay POST'u 303 yerine **200** ile küçük bir ara sayfa
  döner — *"Continue to Malta Calculator"* bağlantısı (`<a href>` — CSP gezinmeyi kısıtlamaz) ve
  `<meta http-equiv="refresh" content="0;url=…">`. Bedeli bir ek tıklama ya da bir anlık ara ekran; kod
  zaten basılmış olduğundan bağlantı 60 sn içinde izlenmelidir. Ara sayfa da `no-store`,
  `no-referrer`, `frame-ancestors 'none'`.
- `frame-ancestors 'none'` + `X-Frame-Options: DENY` (clickjacking — §16).
- `Cache-Control: no-store`, `Referrer-Policy: no-referrer` (onay sayfasının URL'si istemci adını ve
  `state`'i taşır).

## 5. Token ucu (`POST /oauth/token`)

### 5.1 İstemci kimliği

- Yalnız `application/x-www-form-urlencoded` (A2), gövde ≤ 8 KiB; istemci kimliği **HTTP Basic**
  (`client_secret_basic`, RFC 6749 §2.3.1 — A2): `Authorization: Basic base64(client_id:client_secret)`
  (bizim değerlerimiz base64url alfabesindedir, form-kodlama sorunu doğmaz).
- Gövdeyle bileşimler (ORTA-3 (b)):

  | Basic | Gövdede | Sonuç |
  |---|---|---|
  | var | yok | kabul |
  | var | `client_id` = Basic'inki | kabul (RFC 6749 §4.1.3 gizli istemcide `client_id`'yi gövdede yasaklamaz) |
  | var | `client_id` ≠ Basic'inki | `invalid_request` |
  | var | `client_secret` (değeri ne olursa olsun) | `invalid_request` (RFC 6749 §2.3: istemci birden çok yöntem **kullanmamalı**) |
  | yok | herhangi bir şey | 401 `invalid_client` |

- Kimlik yanlış / istemci `disabled` → **401 `invalid_client`** + `WWW-Authenticate: Basic realm="taptime"`
  (RFC 6749 §5.2).
- Sıra (Bearer zincirinin kalıbı — `chain.go:10-26`): biçim (DB'siz) → adres kovasında **`Reserve`**
  (dolu → 429, çözümleyici çağrılmaz) → istemci çözümleyicisi (§14.3) → kimlik reddedilirse **`Keep`**
  (sayılı kalır), kabul edilirse **`Refund`** (iade) + **istemci kovası** (§8.7, T90 kalıbı).
  `Authorization` okunduktan hemen sonra istekten silinir.
- Secret karşılaştırması çözümleyicide hash eşitliğiyle (`secret_ok boolean` döner, hash dönmez).

### 5.2 `grant_type=authorization_code`

Parametreler: `code`, `code_verifier` (A2; RFC 7636 §4.1: 43–128 karakter, `[A-Za-z0-9-._~]`),
`redirect_uri` **isteğe bağlı** (ORTA-3 (a)): gelirse kodun `redirect_uri`'siyle **bayt bayt** aynı
olmalı, gelmezse kabul.

> **`redirect_uri` — RFC 6749'a göre bilinçli gevşeme.** RFC 6749 §4.1.3 yetkilendirme isteğinde
> `redirect_uri` verilmişse token isteğinde onu **zorunlu** kılar (biz yetkilendirmede onu her zaman
> istiyoruz, V2). Kodun yönlendirmeye bağlanmasının amacı, başka bir adrese sızdırılmış kodun takasını
> önlemektir; bu bağ burada **PKCE** (verifier'ı yalnız akışı başlatan istemci bilir) **ve** gizli istemcinin
> secret'ı ile zaten kurulur ve yönlendirme adresi kayıtlı bir listeden bayt bayt seçilmiştir. OAuth 2.1
> taslağının bu bağı PKCE lehine gevşettiği bilinir; taslağın güncel metni bu ADR için **okunmadı**. Addendum
> istemcinin `redirect_uri`'yi token isteğinde gönderip göndermediğini söylemiyor (A2) — iki durumda da
> çalışmak bu yüzden seçildi.

1. Kod biçimi (önek + 43) — değilse `invalid_grant`, DB'siz.
2. `resolve_oauth_code_by_hash` → `(tenant_id, code_id, client_id, …)`; yoksa `invalid_grant`.
3. `WithTenant(kodun tenant'ı)` içinde **tek ifadeyle tüketim** (CLAUDE.md §4.4'ün ruhu — oku-sonra-yaz yok):
   ```sql
   UPDATE oauth_codes SET consumed_at = clock_timestamp()
    WHERE id = @code_id AND tenant_id = @tenant_id
      AND consumed_at IS NULL AND expires_at > clock_timestamp()
   RETURNING client_id, redirect_uri, code_challenge, scopes, created_by;
   ```
   **Kod, sonuç ne olursa olsun bu ilk sunumda tüketilir**: adım 5–6'nın retlerinde işlem tüketimi
   **commit eder**, sonra `invalid_grant` döner. Böylece bir kod üzerinde `code_verifier` denemesi **tek
   atış**tır. (Tek istisna adım 7'nin `UNIQUE` yarışıdır: orada işlem geri alınır.)
4. 0 satır: kod tüketilmiş ve bir grant doğurmuşsa (`grant_id IS NOT NULL`) → o grant **iptal**
   (`revoked_reason='code_reuse'`, RFC 6749 §4.1.2'nin SHOULD'u) + `invalid_grant`; süresi dolmuşsa
   `invalid_grant`.
5. Satırın `client_id`'si kimliği doğrulanan istemci değil → `invalid_grant`. `redirect_uri` geldiyse ve
   eşit değilse → `invalid_grant`. `BASE64URL(SHA256(code_verifier)) ≠ code_challenge` (sabit zamanlı
   karşılaştırma) → `invalid_grant`.
6. Tenant askıda → `invalid_grant` (§11.2). Onaylayan üyelik artık aktif owner değil → `invalid_grant`
   (60 sn'lik pencerede rol düşürülmüşse).
7. Aynı işlemde: (tenant, istemci) için canlı grant varsa iptal (`superseded`); yeni `oauth_grants` satırı
   (kapsam = kodun kapsamı, `created_by` = onaylayan üyelik); `oauth_codes.grant_id` yazılır; erişim +
   refresh tokenı; tenant `audit_log`'a `oauth.grant_created` (aktör: onaylayan üyelik — §8.7). Aynı
   (tenant, istemci) için iki farklı kodun yarışı kısmi `UNIQUE`'e çarpar → kaybeden `invalid_grant`
   (işlemi geri alınır; kodu tüketilmemiş kalır ve 60 sn içinde yeniden denenebilir).

### 5.3 `grant_type=refresh_token` — rotasyon, tolerans, yarış (OA-5 = (b))

Parametreler (A2): `refresh_token`; `scope` **isteğe bağlı** (ORTA-3 (d), RFC 6749 §6):

- `scope` yoksa → yeni erişim tokenı grant'ın kapsamını taşır;
- `scope` varsa → boşlukla ayrık bir **küme** olarak okunur (sıra önemsiz, tekrar tek sayılır); **boş küme**
  (`scope=` ya da yalnız boşluk) → `invalid_scope`; grant'ın kapsamının **alt kümesi** olmalı (eşitlik dahil),
  aksi `invalid_scope` (genişletme yok); yeni erişim tokenı **bu alt kümeyi** taşır ve `oauth_tokens.scopes`'a
  yazılır (§14.2); `/v1`'in kapsam kontrolü **tokenın kendi kapsamını** okur, grant'ınkini değil (§14.3);
- yeni refresh tokenı **her zaman grant'ın kapsamını** taşır (RFC 6749 §6: yeni refresh tokenının kapsamı
  eskisininkiyle aynı olmalı) — daraltma yalnız o erişim tokenına uygulanır;
- yanıttaki `scope` erişim tokenının kapsamıdır (RFC 6749 §5.1, A3).

Adımlar:

1. Biçim → `resolve_oauth_token_by_hash` → `kind='refresh'` değilse ya da istemci tutmuyorsa
   `invalid_grant`.
2. `WithTenant` içinde önce grant satırı kilitlenir (aynı grant'ın bütün refresh'leri sıraya girer):
   ```sql
   SELECT id, scopes FROM oauth_grants
    WHERE id = @grant_id AND tenant_id = @tenant_id AND revoked_at IS NULL
    FOR UPDATE;
   ```
   0 satır → `invalid_grant`.
3. **Tek ifadeli koşullu döndürme** (`UPDATE tags SET last_ctr … WHERE last_ctr < $2` kalıbı):
   ```sql
   UPDATE oauth_tokens SET used_at = clock_timestamp()
    WHERE id = @token_id AND tenant_id = @tenant_id AND kind = 'refresh'
      AND used_at IS NULL AND revoked_at IS NULL AND expires_at > clock_timestamp()
   RETURNING grant_id;
   ```
   1 satır → aynı işlemde yeni refresh + yeni erişim tokenı; yanıt. Eski erişim tokenı süresi dolana dek
   (≤ 1 sa) geçerli kalır — uçuştaki istekleri kırmamak için.
4. 0 satır ve token **kullanılmış** (`used_at IS NOT NULL`, iptal edilmemiş) → **yeniden kullanım**:
   - `clock_timestamp() - used_at <= 30 sn` → **tolerans**: grant kilidi altında grant'ın **öteki bütün** canlı
     tokenları — her **kullanılmamış** refresh **ve** her erişim tokenı, hangi soydan gelirse gelsin — tek
     ifadeyle iptal edilir; **kullanılmış refresh tokenları (sunulan R0 dahil) dışarıda kalır**:
     ```sql
     UPDATE oauth_tokens SET revoked_at = COALESCE(revoked_at, clock_timestamp())
      WHERE tenant_id = @tenant_id AND grant_id = @grant_id AND revoked_at IS NULL
        AND expires_at > clock_timestamp()
        AND NOT (kind = 'refresh' AND used_at IS NOT NULL);
     ```
     ve **yeni tek bir çift** verilir. R0'ın `used_at`'i **değişmez** (pencere ilk kullanımdan sayılır, her
     sunumda uzamaz). Audit satırı yok (normal bir yeniden deneme), sayaç log'u var. R0 pencere içinde
     **tekrar tekrar** sunulabilir; her sunum aynı kuralla öteki her şeyi iptal edip tek yeni çift verir.
   - `> 30 sn` → **grant iptali** (`revoked_reason='refresh_reuse'`) — tolerans yoluyla daha önce sunulmuş R0
     dahil —, bütün tokenları ölür, `audit_log`'a `oauth.refresh_reuse_detected`; `invalid_grant`.
5. 0 satır, token **iptal edilmiş** ya da süresi dolmuş → `invalid_grant` (grant iptali yok — toleransla iptal
   edilmiş bir tokenın sunulması, sırayı kaybeden bir isteğin elinde kalan tokendır). Kullanılmış refresh
   tokenları tolerans ifadesinin dışında kaldığı için bir kullanılmış token **hiçbir zaman** bu dala düşmez;
   o her zaman adım 4'tedir.

**Değişmez:** her an bir grant'ın **en çok bir** canlı (kullanılmamış, iptal edilmemiş, süresi dolmamış)
refresh tokenı ve en çok bir **tolerans sonrası** canlı erişim tokenı vardır. Normal döndürme ilkini korur
(eskisi `used_at` alır, yenisi doğar; eski erişim tokenı ≤ 1 sa yaşar); tolerans ikisini de **soydan bağımsız**
korur — R0→R1→R2'den sonra R0 30 sn içinde gelirse R2 ve bütün erişim tokenları iptal olur, R3 doğar; R0, R1
kullanılmış ve iptalsiz kalır (pencereleri kendi `used_at`'lerinden sayılır). İki canlı soy kalmaz. Bu yüzden
`oauth_tokens`'ta soy sütunu (`parent_id`) **yoktur** (kural soyu okumaz — §14.2).

**Eşzamanlılık kanıtı (uygulama turunun testi):** aynı refresh tokenıyla N ≥ 3 goroutine → adım 2'nin kilidi
onları sıraya sokar; ilki adım 3'te 1 satır döner (normal döndürme); geri kalan N−1'in **her biri** adım 4'ün
toleransına girer ve **başarılı** olur — **N'nin N'si de bir çift alır**; sonunda **tam olarak bir** canlı
refresh tokenı ve ona eşlik eden **tek** canlı erişim tokenı kalır (son verilen çift). R0→R1→R2 sonrası R0
sunumu aynı sonucu verir; R0'ın ilk kullanımından 30 sn sonra gelen sunum grant'ı iptal eder. `-race` ile.
Kilit şarttır: kilitsiz, iki eşzamanlı tolerans iki canlı çift doğurabilirdi.

**Token ucunun sunucu tavanı D (ORTA-2).** Sıra bozulan bir yeniden deneme şu biçimde meşru istemciyi
koparabilir: asıl istek O (R0) gecikir; yeniden deneme Rt kilidi önce alır ve R0'ı normal döndürür (R1
istemciye ulaşır); O sonra tolerans yoluna girer, R1'i iptal edip R1''yi verir — ama R1'' istemcinin bıraktığı
bağlantıya gider; istemcinin elindeki R1 iptal edilmiştir, bir sonraki refresh adım 5'te `invalid_grant` —
bağlantı **kalıcı** kopar. Karşılık:

- **D = 10 sn:** token ucunun (`/oauth/token`, `/oauth/revoke`) her isteği, sunucuya varışından itibaren
  `context.WithTimeout(D)` altında koşar; işlem **commit'ten hemen önce** bağlamı denetler — bağlam iptal
  edilmişse (istemci bağlantıyı bıraktı) ya da D aşılmışsa işlem **geri alınır**, hiçbir token doğmaz ve
  `used_at` yazılmaz. Grant kilidi ve sorgular da aynı bağlamla iptal edilir. D, router'ın genel 30 sn
  tavanının (`internal/httpx/router.go:32`) altındadır.
- **İstemci kuralı (§18.2):** zaman aşımından sonra bağlantıyı kapat, **≥ D (10 sn) bekle**, asıl isteği
  gönderdikten sonraki **30 sn içinde** yeniden dene; aynı refresh tokenıyla **eşzamanlı** istek yapma. Bu
  kuralla O, Rt sunucuya vardığında ya commit etmiştir (Rt toleransa girer, istenen durum) ya da geri
  alınmıştır (Rt normal döndürür); O'nun Rt'den **sonra** commit etmesi kalmaz.
- **Kalan durum, adlandırılmış:** (i) bağlamın denetlenmesi ile `COMMIT`'in sunucuda tamamlanması arasındaki
  milisaniyelik aralık; (ii) istemcinin bağlantıyı bırakmasının ingress üzerinden uygulamaya iptal olarak
  ulaşması — ingress-nginx'in istemci kopuşunda üst akış bağlantısını kapattığı varsayılıyor, **doğrulanmadı**
  (uygulama turunda ölçülür); ulaşmazsa koruma yalnız D'dir; (iii) kuralı çiğneyen istemci (eşzamanlı refresh,
  D'den önce yeniden deneme) bağlantısını kalıcı koparabilir — owner yeniden onaylar. Bunlar sessiz değildir:
  istemci `invalid_grant` görür, owner *"Connected apps"*te son kullanımı görür.

**Bedeli, adlandırılmış (orkestratör kararı (öneri), kullanıcıya raporlandı).** Meşru istemci ile refresh
tokenını **ve** client secret'ı ele geçirmiş bir saldırgan 30 sn'lik pencerede yarışırsa **sonuncu gelen**
tek çifti alır; öteki bir sonraki isteğinde `401`/`invalid_grant` görür. Meşru istemci kaybederse bağlantı
kopmuş görünür ve yeniden onay ister; owner *"Connected apps"*te son kullanımı görür ve Disconnect edebilir.
Pencerenin dışında aynı yeniden kullanım grant'ı iptal eder. Gizli istemcide refresh tokenı tek başına işe
yaramadığından (Basic secret ister) bu yarış yalnız secret sızmışsa vardır.

**Gerekçe (kullanıcının seçimi (b)), daraltılmış.** Tolerans **tek bir durumu** kurtarır: asıl istek commit
etmiş, yanıtı kaybolmuş ve istemci kurala uyarak (≥ D bekleyip 30 sn içinde) yeniden denemiştir — katı
tespitte (a) bu bağlantıyı sessizce koparırdı. Tolerans, aynı refresh tokenını **sıra gözetmeden** kullanan iki
işçiyi **korumaz**: sırayı kaybeden işçinin elindeki çift iptal edilebilir ve bağlantı kopabilir; bunu yalnız
istemci tarafında tek uçuş (single-flight) önler. 30 sn, bir ağ zaman aşımı + D + yeniden denemeyi kapsar.

Temizlik **tenant içinde, tembel** (K-19 — tenant'lar arası tarayan işçi yok, ADR 0028 §6): refresh
işleminde o grant'ın süresi 1 saatten çok önce dolmuş erişim tokenları ve 90 günden eski kullanılmış /
iptal edilmiş refresh satırları silinir.

### 5.4 Yanıt ve hata biçimi

Başarı (A3, RFC 6749 §5.1):

```json
{"access_token": "tt_at_live_…", "token_type": "Bearer", "expires_in": 3600,
 "refresh_token": "tt_rt_live_…", "scope": "timesheets:read employees:read employees:write"}
```

Başlıklar: `Content-Type: application/json`, `Cache-Control: no-store`, `Pragma: no-cache` (RFC 6749 §5.1).

Hata (A4, RFC 6749 §5.2): JSON `{"error": "<kod>", "error_description": "<sabit İngilizce cümle>"}`.
`error_description` girdi yankılamaz; addendum'a göre istemci onu göstermez (A4).

| Durum | HTTP | `error` | Dayanak |
|---|---|---|---|
| eksik/biçimsiz parametre, Basic + gövde çakışması | 400 | `invalid_request` | RFC 6749 §5.2 |
| istemci kimliği | 401 | `invalid_client` + `WWW-Authenticate: Basic` | RFC 6749 §5.2 |
| kod/refresh geçersiz, süresi dolmuş, başkasının, PKCE/`redirect_uri` uyuşmuyor, askı | 400 | `invalid_grant` | RFC 6749 §5.2 |
| `grant_type` ne `authorization_code` ne `refresh_token` | 400 | `unsupported_grant_type` | RFC 6749 §5.2 |
| refresh'te kapsam genişletme / bilinmeyen kapsam | 400 | `invalid_scope` | RFC 6749 §5.2 |
| oran kovası dolu | **429** + `Retry-After` | **`temporarily_unavailable`** | **SAPMA** — §5.2'nin kümesinde yok (§18) |
| bizim tarafımızda hata (DB, panik) | **503** | **`temporarily_unavailable`** | **SAPMA** — §5.2'nin kümesinde yok (§18) |

`temporarily_unavailable` RFC 6749'da yalnız **yetkilendirme ucunun** hata kümesindedir (§4.1.2.1); token
ucunun kümesinde (§5.2) 429/503 için bir kod yoktur. RFC'nin kendi sözlüğünden en yakın değeri, **HTTP
durumuyla birlikte** (`Retry-After` 429'da) kullanmak seçildi: kodu tanımayan bir istemci yine HTTP
durumunu görür. Malta Calculator'a issue yorumunda bildirilir.

**`/oauth/*` hata biçimi `/v1`'in §8 problem biçimi DEĞİLDİR** (`{code,message,details}`); rehber ikisini
ayrı anlatır.

## 6. Revoke (`POST /oauth/revoke`, RFC 7009 — A1)

- Form gövdesi `token=…` (A1) (+ isteğe bağlı `token_type_hint`, yok sayılır — önek türü söyler); istemci
  §5.1'deki gibi HTTP Basic.
- **Refresh tokenı** → grant iptal (`revoked_reason='client'`) — bütün tokenlar; *"Connected apps"*
  listesinden düşer; `audit_log`'a `oauth.grant_revoked`. **Erişim tokenı** → yalnız o token.
- Bilinmeyen, biçimsiz, zaten iptal edilmiş tokenda **200** (RFC 7009 §2.2). **Başka istemcinin**
  tokenında da **200, hiçbir şey yapılmaz** — RFC 7009 §2.1 bu durumda reddi öngörür; reddetmek bir tokenın
  başka bir istemcide **var olduğunu** söyleyen bir kehanet olurdu (§18 — sapma).
- Yanıt: 200, boş gövde. Oran ve kimlik zinciri token ucununki.

## 7. Token modeli

### 7.1 Biçim ve önek

Kalıp `internal/apikey`'inkidir (`token.go:39-62`): önek + 43 karakter katı base64url (32 bayt
`crypto/rand`), bir dağıtım **tek** ortam önekini kabul eder, yanlış önek **DB'ye gitmeden** reddedilir.

| Değer | Prod | Sandbox | Nerede görünür |
|---|---|---|---|
| Client secret | `tt_cs_live_` + 43 | `tt_cs_test_` + 43 | operatör ekranında **bir kez** |
| Yetki kodu | `tt_ac_live_` + 43 | `tt_ac_test_` + 43 | tarayıcı URL'si → istemci sunucusu |
| Erişim tokenı | `tt_at_live_` + 43 | `tt_at_test_` + 43 | `Authorization: Bearer` |
| Refresh tokenı | `tt_rt_live_` + 43 | `tt_rt_test_` + 43 | yalnız token ucu |
| `client_id` (sır değil) | `app_` + UUID | `app_` + UUID | URL, panel, audit |

- API anahtarı (`tt_live_` + 43 = 51 karakter) ile erişim tokenı (`tt_at_live_` + 43 = 54 karakter) biçimden
  ayrılır; Bearer zinciri hangisi olduğunu DB'siz bilir (§8.1).
- **Sandbox ayrı önek taşır**, anahtarın gerekçesiyle (K-10): yanlış ortamın tokenı DB'siz 401 ve R7d hangi
  ortamın sırrının sızdığını söyler. Ortamlar ayrı veritabanı ve ayrı HMAC anahtarı taşır; önek tek savunma
  değildir.
- **R7d genişler:** `tt_((cs|ac|at|rt)_)?(live|test)_|tt_whsec_` (bugün `tt_(live|test|whsec)_` —
  `secretscan.sh:486` ön süzgeç, `:898` eşleşme, `:903` gövde soyma — üçü birlikte değişir); `tt_at_live_x…`
  bugünkü kalıba **uymaz** (`tt_` ardından `at_` gelir) —
  genişletmeden yeni sırlar taranmaz. Kasıtlı ihlalde kırmızı testiyle.

### 7.2 Saklama

- Saklanan değer: hex HMAC-SHA256(`TAPPA_OAUTH_HMAC_KEY`, alan etiketi ‖ değer); etiketler
  `tappa/oauth/secret/v1|`, `tappa/oauth/code/v1|`, `tappa/oauth/access/v1|`, `tappa/oauth/refresh/v1|`,
  `tappa/oauth/request/v1|` (bekleyen istek çerezi), `tappa/oauth/request-digest/v1|` (`rd`). Yeni env
  anahtarı `namedKeys()`'e girer ve bütün anahtarlardan farklı olmak zorundadır
  (`internal/config/config.go:1211-1214`).
- `token_hash` ve `code_hash` **küresel UNIQUE** (çözümleyiciyi bir satıra bağlar — md.7 (ii)).
- Go tarafında değerler `apikey.Token` gibi **yazdırılamayan** tiplerde taşınır (Format/String/GoString/
  LogValue/MarshalText → yer tutucu); tek açığa çıkarma noktaları adıyla: token yanıtı, onay 303'ü (kod),
  operatör ekranı (secret).
- **KEK yok:** hiçbir OAuth sırrının geri açılması gerekmez (webhook imza anahtarının aksine — ADR 0028 §6);
  yalnız hash saklanır. CLAUDE.md §4.7'nin AES anahtarı sınıfına girmez.

### 7.3 Ömürler

| Değer | Ömür | Gerekçe |
|---|---|---|
| Yetki kodu | **60 sn**, tek kullanım; şemada eşitlik CHECK'i, tek zaman kaynağı `now()` (§14.2) | İstemci sunucusu kodu anında takas eder; RFC'nin 10 dk tavanından çok kısa |
| Erişim tokenı | **1 sa** (`expires_in: 3600`); şemada eşitlik CHECK'i | Issue'nun önerisi; iptal zaten anında (çözümleyici her istekte grant'a bakar) |
| Refresh tokenı | **90 gün** = `2160 hours` (her yeni refresh tokenı 90 gün yaşar → *"90 gün boşta"*); şemada eşitlik CHECK'i; grant kopana dek mutlak tavan yok | Bordro aylık koşar; mutlak tavan bağlantıyı **sessizce** kırardı (T174'ün gerekçesi) |
| Bekleyen istek çerezi | 10 dk | Bir giriş için yeter |
| Refresh toleransı | 30 sn (§5.3) | Kullanıcı kararı OA-5 (b) |

**İptal önbelleksiz:** erişim tokenının çözümleyicisi her istekte grant'ın `revoked_at`'ini ve istemcinin
`disabled_at`'ini taşır (§14.3) — *Disconnect*, revoke ve operatörün istemciyi kapatması **bir sonraki
istekte** geçerli (ADR 0028 §3'ün anahtar kuralı).

### 7.4 Asla loglanmayanlar (CLAUDE.md §7'ye eklenecek)

Client secret, yetki kodu, erişim ve refresh tokenı, bunların hash'leri, `code_verifier`, `state`,
`Authorization` (Basic ve Bearer) başlığı, bekleyen istek çerezinin değeri ve `rd` — hata mesajı ve audit
`detail`'i dahil. Log ve audit yalnız `client_id`, grant id, token id, tenant id ve sabit sebep sınıfı taşır.
`/oauth/token` gövde alanlarının adları (`code_verifier`, `refresh_token`, `client_secret`, `code`) R7'nin dar
kalıbına eklenir.

## 8. `/v1` tarafı

### 8.1 Zincir

Bugünkü sıra korunur (`chain.go:10-26`); 2., 3., 4. ve 5. adım iki türe ayrılır:

| # | Adım | API anahtarı (**değişmez**) | OAuth erişim tokenı |
|---|---|---|---|
| 1 | `Authorization: Bearer` | aynı | aynı |
| 2 | biçim (DB'siz) | `tt_live_`+43 | `tt_at_live_`+43 · ikisi de değilse 401 |
| 3 | adres kovası (**ortak** limiter, 1200/dk) | **her istek sayılır** (bugünkü `chargeAddress`), dolu → 429 | **`Reserve`** (şarj + karar tek kilitte — T90); dolu → 429, çözümleyici çağrılmaz (§8.7) |
| 4 | çözümleyici | `resolve_api_key_by_hash` → iptal 401 · K-22 kapalı 403 `api_disabled` | `resolve_oauth_token_by_hash` → bilinmiyor / `kind≠access` / süresi dolmuş / token ya da grant iptal / istemci kapalı → **401** (`WWW-Authenticate: Bearer realm="taptime", error="invalid_token"`) ve rezervasyon **`Keep`**; işletmenin API'si **operatörce kapatılmış** (§11.4) → **403 `api_disabled`** ve `Keep`; kabul → **`Refund`**. Hiç açılmamış K-22'ye bakılmaz (OA-1) |
| 5 | kimlik kovaları | anahtar başına 600/dk | **grant başına 600/dk** + **istemci başına 6000/dk** |
| 6 | `last_used_at` | anahtar, dakikada ≤ 1 | grant, dakikada ≤ 1 |
| 7 | K-19 | tenant kümeye | aynı |
| 8 | **kapsam** (yeni) | geçer (tam yetki — bugünkü davranış) | rotanın kapsamı **tokenın kendi kapsamında** (`oauth_tokens.scopes`, refresh'te daraltılmış olabilir) yoksa **403 `insufficient_scope`** |
| 9 | **eş süzgeci** (yeni) | yok (bugünkü davranış) | eşlerin verisi çıkmaz (§8.5) |

`apikey.Principal` genelleşir: `{TenantID, ActorID, Kind (key | grant), ClientID, Scopes, Full bool,
OwnOnly bool}`. Bütün handler'ların tek yetki kaynağı yine `TenantID`'dir; tenant istekten okunmaz.

### 8.2 Kapsam → rota (eksiksiz — `surface.go:176-208` + #36 + bu ADR)

| Rota | Kapsam | Not |
|---|---|---|
| `GET /v1/openapi.yaml`, `/v1/guide.md`, `/v1/guide.tr.md` | — (kimliksiz) | değişmez |
| `GET /v1/business` (**yeni**, §8.4) | **herhangi biri** (A7) | her grant ≥ 1 kapsam taşır |
| `GET /v1/timesheets` (#36, ADR 0046) | `timesheets:read` | yalnız kendi çalışanları (#36'nın sözleşmesi) |
| `GET /v1/employees` | `employees:read` | yalnız kendi çalışanları (RLS) |
| `GET /v1/employees/{ref}` | `employees:read` | |
| `PUT /v1/employees/{ref}` | `employees:write` | `active:false` dahil — K-25 bütçesi grant başına (§8.7); davet → §11.3 |
| `DELETE /v1/employees/{ref}` | `employees:write` | K-25 |
| `POST /v1/employees/{ref}/resend-activation` | `employees:write` | davet → §11.3 |
| `GET /v1/directory` | `employees:read` | **eş yarısı OAuth'a kapalı** (§8.5) |
| `PUT /v1/locations/{ref}` | `locations:write` | |
| `GET /v1/punches` (akış ve `from`/`to` geçmiş sorgusu) | `punches:read` | **eş punch'ları OAuth'a kapalı** (§8.5); `?scope=employer\|location` **punch parametresidir**, OAuth kapsamı değil — rehber ikisini ayrı adlandırır |
| `POST /v1/sandbox/taps` (yalnız sandbox derlemesi) | **hiçbiri — OAuth'a kapalı** | simülasyon `tt_test_` anahtarıyla (§8.6) |
| `GET /v1/sandbox/openapi.json` (yalnız sandbox) | — (kimliksiz) | |

- **Kapsam kümesi kapalı:** `timesheets:read`, `employees:read`, `employees:write`, `locations:write`,
  `punches:read`. `webhooks:manage` bu teslimde **kümede değildir** — kayıtta izinli kapsama giremez, istekte
  `invalid_scope` (§13). **Örtük içerme yok** — `employees:write` `employees:read`'i içermez.
- **Varsayılan kapalı:** kapsam tablosuna girmemiş kimlikli bir rota OAuth tokenına **403**'tür. Uygulama
  turunun testi gönderilen yönlendiriciyi yürür ve kimlikli her rotanın tabloda olduğunu kanıtlar
  (`TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses` emsali — `cmd/tappa/approuter_test.go:410`).

### 8.3 `insufficient_scope`

- **403**, §8 problem gövdesi: `{"code": "insufficient_scope", "message": "This token's grant does not
  include the scope this route needs.", "details": {"scope": "timesheets:read"}}` + başlık
  `WWW-Authenticate: Bearer realm="taptime", error="insufficient_scope", scope="timesheets:read"` (RFC 6750
  §3.1). `details.scope` sabittir (rotanın kapsamı), girdi değildir.
- Kapalı kod kümesine (`errors.go:24-40`, `Codes()`, OpenAPI enum'u) **yeni değer** girer. Yalnız OAuth
  tokenı bu kodu alabildiği için var olan hiçbir anahtar istemcisi onu görmez — v1 içinde kabul edilir,
  rehbere yazılır.
- API anahtarları **hiçbir rotada** `insufficient_scope` almaz (`Full=true`).

### 8.4 `GET /v1/business` (yeni — A7; OA-2 = (a))

```json
{"id": "biz_8f0c…", "name": "Kebab Factory Ltd", "companyCode": "KF"}
```

| Alan | Kaynak | Not |
|---|---|---|
| `id` | `"biz_" + tenants.id` | K-8'in dış kimlik kalıbı (`emp_`/`loc_`/… + UUID) |
| `name` | `tenants.name` | işletmenin kendi bağlamında, RLS + açık `id = @tenant_id` |
| `companyCode` | `tenants.api_code`, **yoksa `null`** | Okundu: Taptime'da *"şirket kodu"* `api_code`'dur (`00038:54-57`; punch'taki `employee.company`/`location.company`, rehber §6.2). Yalnız operatör yazar ve yalnız API'yi açarken zorunludur → OAuth ile bağlanan, API'si açılmamış bir işletmede `null` |

Her kimlik okur (anahtar dahil). Askıda açık (okuma — ADR 0028 §10). OpenAPI + rehberin iki dili +
`TestOpenAPI_TheExamplesAreTheHandlersAnswers` emsali pin.

### 8.5 Holding eşleri — OAuth tokenıyla hiçbir uçtan (OA-4 = (a), ORTA-1)

**Kural:** OAuth tokenıyla holding **eşlerinin** verisi (eşin çalışanı, eşin lokasyonu, eşin şirket kodu)
**hiçbir** `/v1` ucundan çıkmaz. Gerekçe: KF'nin owner'ının bir **üçüncü taraf** uygulamaya verdiği onay,
KM'nin owner'ının onayı olmadan KM'nin verisini o uygulamaya taşırdı. API anahtarında davranış **değişmez**.

Etkilenen uçların tam sayımı (`surface.go:176-208` + #36 + `/v1/business`):

| Uç | Eş verisi bugün çıkıyor mu | OAuth'ta |
|---|---|---|
| `GET /v1/directory` | **evet** — eşlerin çalışanları (`holding_directory` — `directory.go:23-27`) | eş yarısı **çağrılmaz**; yalnız kendi çalışanlarının RLS'li okuması |
| `GET /v1/punches?scope=employer` (akış + geçmiş) | **evet** — kendi çalışanının eş plaketindeki punch'ı, **eşin lokasyonuyla** (`punches.go:23-24`) | yalnız `roles = '{employer,location}'` satırları (kendi çalışanı, kendi plaketi) |
| `GET /v1/punches?scope=location` (akış + geçmiş) | **evet** — kendi plaketindeki eş çalışanının punch'ı, **eşin çalışanıyla** (`punches.go:25-26`) | yalnız `roles = '{employer,location}'` satırları |
| `GET /v1/timesheets` (#36) | hayır — *"Employer scope only: the business's own employees"* (#36); çalışanın eş plaketinde çalıştığı süre **kendi çalışanının** süresidir ve lokasyon alanı taşımaz | değişmez — ADR 0046'nın uygulaması bunu kendi testinde kanıtlar (bu ADR için **doğrulanmadı**, #36 paralel dalda) |
| `GET /v1/employees`, `/{ref}` | hayır (RLS, kendi tenant'ı) | değişmez |
| `GET /v1/business` | hayır | değişmez |
| `PUT`/`DELETE` yazmaları | hayır (kendi tenant'ında `ref` araması) | değişmez |
| webhook | — | OAuth'ta yok (§13) |

**Punch süzgecinin biçimi:** `db/queries/punchfeed.sql`'in iki sorgusu (`:47` akış, `:121` geçmiş) `@role =
ANY (pe.roles)` taşır; ikisine `AND (NOT @own_only::boolean OR pe.roles = '{employer,location}'::text[])`
eklenir (`roles`'un üç değerli kümesi — `00048:167`). Akışın imleci ve sırası değişmez (imleç bir
konumdur; atlanan satırlar delik bırakır, akış sözleşmesi zaten deliklere izin verir — rehber §6.4).
Bedeli, adlandırılmış: OAuth ile `punches:read` alan bir uygulama, kendi çalışanının eş plaketindeki
punch'ını görmez; çalışılan süre için Taptime'ın raporu ve `timesheets` esastır (rehber §6.5).

### 8.6 Sandbox

- Sandbox derlemesi aynı OAuth yüzeyini taşır; `_test_` önekleri; kendi operatörü, kendi kaydı, kendi
  `TAPPA_OAUTH_HMAC_KEY`'i. **Canlı sandbox yok** (kullanıcı, 2026-11-14): deneme yerel sandbox derlemesiyle
  (`make build-sandbox` — `Makefile:98-104`; `TAPPA_ENV=sandbox`). Yerel kayıt ve callback §10.4'te.
- `POST /v1/sandbox/taps` OAuth tokenına kapalı (§8.2): simülasyon bir geliştirme aracıdır; uygulama
  geliştiricisi yerel sandbox'ta kendi işletmesinin `tt_test_` anahtarını kullanır.

### 8.7 Oran sınırı, audit atfı, bütçe, idempotency, askı (ORTA-2)

**Gerçek bedel (okundu).** Adres kovası çözümleyiciden **önce** ve **her istekte** sayılır, istemci IP'sine
anahtarlıdır (`chain.go:59`, `surface.go:47-52`). Tek çıkış IP'li, çok işletmeli bir istemci (Malta
Calculator) bütün işletmelerinin trafiğini **tek** adres kovasında paylaşır: `/v1`'de **1200/dk toplam**,
token ucunda (ilk taslağın sayısı) **60/dk toplam**. Örnek: ay sonu bordrosu 50 işletme × (1 timesheets +
30 çalışan `PUT` + rehber sayfaları) ≈ 1700 istek bir patlamada → 1200/dk'da 429; 500 işletmenin tokenları
saat başında yenilenirse token ucunda 500/dk → 60/dk'da 429. "Grant başına 600/dk" bu yüzden ilk taslakta
gerçek bir tavan **değildi** — adres kovası ondan önce doluyordu.

**Karar (orkestratör kararı (öneri), kullanıcıya raporlandı) — T90'ın kalıbı.** *"Önce `Allowed`, sonra
`Charge`"* biçimi T90'da ölçülüp kaldırıldı (iki kilitli adım; aradaki iş sırasında eşzamanlı N istek
bütçeyi N kez geçer — `docs/backlog.md:263`) ve `Allowed`'ın üretimde çağrılmaması pinlidir
(`cmd/tappa/limiterallowed_test.go:22`). Başarısızlıkta sayılan bir bütçenin doğru biçimi **`Reserve` →
iş → `Keep` / `Refund`**'dur (`internal/httpx/ratelimit.go:195-253`): `Reserve` şarjı ve kararı **tek
kilitte** yapar, kabul edilen olay uçuştayken de bir birim tutar, ret şarj edilir ve iade edilmez.

| Yüzey | Adres kovası | Kimlik reddedilirse | Kimlik kabul edilirse |
|---|---|---|---|
| `/v1`, **anahtar** biçimi | bugünkü gibi her istek sayılır (`chargeAddress`) — **değişmez** | (sayılmış) | anahtar 600/dk — **değişmez** |
| `/v1`, **token** biçimi | **`Reserve`** aynı limiter'da (1200/dk, anahtarla **ortak**) — dolu → 429, çözümleyici yok | **`Keep`** | **`Refund`** + grant 600/dk **ve** istemci 6000/dk |
| `/oauth/token`, `/oauth/revoke` | **`Reserve`** ayrı limiter'da (60/dk) — dolu → 429 `temporarily_unavailable` | **`Keep`** (yanlış Basic, kapalı istemci) | **`Refund`** + istemci 600/dk |
| `/v1` kimliksiz belgeler | bugünkü gibi | — | — |

- **Kesin sınır (DoS koruması zayıflamaz).** Bir pencerede bir adresin tuttuğu birim sayısı (tutulmuş +
  uçuştaki) her an ≤ sınırdır, çünkü kabul ancak şarjdan sonraki sayı sınırın içindeyse verilir ve aynı
  kilitte olur. Dolayısıyla bir pencerede bir adresten çözümleyiciye ulaşan **reddedilmiş** kimlik sayısı
  **en çok 1200** (`/v1`, anahtar ve token biçimi **toplamı** — ortak limiter) ve **en çok 60** (token
  ucu)'dur — bugünkü anahtar yolunun sınırıyla aynı. Ayrıca her an bir adresten **en çok 1200** istek
  çözümleyicide ya da sonrasında uçuşta olabilir (uçuştaki istek birim tutar).
- **Bedeli, adlandırılmış.** (i) Ret şarj edilir ve iade edilmez: bir adres pencerede sınırı aşan çöp
  gönderirse, o pencerenin kalanında o adresin **başarılı olacak** istekleri de 429 alır — paylaşılan bir
  çıkış IP'sinde bunu yalnız aynı IP'nin sahibi (Malta Calculator'ın kendisi, ör. süresi dolmuş tokenla
  döngüye giren bir işçi) tetikleyebilir. (ii) 1200 eşzamanlı uçuştaki istek bir sonrakini bekletir —
  tek replikalı bir sunucu için ulaşılmaz bir eşzamanlılık. (iii) Pencere istek sürerken dönerse iade
  yeni pencereye yazılmaz (`Refund`'un kendi penceresi kuralı) — zararsız.
- **Başarılı trafiğin tavanı kimlik başınadır:** bir grant 600/dk (anahtarla aynı), bir istemcinin bütün
  grant'ları **6000/dk** (100/sn). 6000'in gerekçesi: bugün kimliksiz bir saldırgan 5 adresten aynı yükü
  zaten üretebilir (5 × 1200); operatörün onayladığı tek bir istemciye bundan fazlası verilmez. Malta
  Calculator'ın 50 işletmelik ay sonu patlaması (~1700) bir dakikada geçer; 500 işletmelik saatlik yenileme
  token ucunun 600/dk istemci kovasında saat içine yayılır (yenileme tokenın süresine bağlı, işletmelerin
  bağlanma anlarına dağılır).
- **Audit atfı (K-23 genelleşir):** API kaynaklı audit satırı `actor_id = Principal.ActorID` (anahtar id
  **ya da** grant id), `detail.via = "api"` (değişmez — `CountAPIDeactivations` `detail->>'via' = 'api'` ile
  sayar, `audit.sql:269-279`), grant'ta ek olarak `detail.client_id`. `audit_log.actor_id` FK'siz.
- **K-25 bütçesi grant başına:** kilit anahtarı `'api-deactivation-budget:' || ActorID` — sorgu değişmez.
  Owner'ın bütçe uyarısı (`ListAPIBudgetAlerts`, `audit.sql:292+`) anahtar adını `api_keys`'ten LEFT JOIN'le
  okur; grant satırları için sorgu `oauth_grants`'a LEFT JOIN'le yalnız `client_id`'yi döndürür ve **ad Go'da
  `resolve_oauth_client` ile** alınır (`tappa_app` `oauth_clients`'ı okuyamaz — §10.1; adı grant satırına
  kopyalamak ikinci bir kaynak olurdu ve `op_*`'ın yeniden adlandırmasını görmezdi). Çözümleme pencerede
  görünen farklı istemci başına bir kez.
- **Idempotency:** `api_idempotency.key_id` `api_keys`'e FK'li (`00052:54-55`) — §14.5'te şema değişikliği.
- **Askı:** ADR 0028 §10'un tablosu grant'a **birebir** uygulanır (kontrol noktası yazmanın kendi
  işleminde, tenant'tan okur — principal türünden bağımsız).

## 9. Tenant izolasyonu (CLAUDE.md §4.5)

- **Grant → tam olarak bir tenant.** `oauth_grants.tenant_id` NOT NULL; kod, erişim ve refresh tokenı
  grant'a **bileşik FK** ile (`(grant_id, tenant_id)` → `oauth_grants(id, tenant_id)`) bağlı.
- **Token doğrulaması tenant bilinmeden yapılmak zorunda** — API anahtarıyla aynı problem, aynı çözüm:
  `tappa_resolver` sahipli, ADR 0002 md.7'nin beş kısıtına uyan SECURITY DEFINER çözümleyici; girdi yalnız
  hash; dönüş küresel UNIQUE `token_hash`'ten **≤ 1 satır**; *"taşı, uygulama"*; önbelleksiz (`00051:158-178`
  kalıbı). Sonraki **her** sorgu `WithTenant(principal.TenantID)` + açık `tenant_id` filtresiyle.
- **RLS değişmez.** Grant, kod ve token tabloları **tenant tablosudur** (CLAUDE.md §6'nın beşlisi + GRANT).
  Uygulama kaydı (`oauth_clients`) **tenant kapsamsızdır** ama **tenant verisi taşımaz**. Operatör bir
  istemciyi kapattığında **hiçbir tenant satırına dokunmaz** — çözümleyici `disabled_at`'i okur. **CLAUDE.md
  §4.5'e dördüncü bir istisna eklenmez.**
- **Holding:** OAuth tokenı ADR 0027'nin eş yüzeyine **hiç** ulaşmaz (§8.5) — tenant sınırını aşan tek
  `/v1` okuması (`holding_directory`) OAuth'ta çağrılmaz.
- **Onayda tenant formdan gelmez** (§4.3); işletme seçimi var olan geçiş mekanizmasıdır.
- **Operatör hangi işletmelerin bir uygulamaya bağlı olduğunu görmez** (v1) — tenant-ötesi okuma olurdu;
  kapsam dışı (§21).

## 10. Uygulama kaydı (app registry — OA-7 = (a))

### 10.1 Tablo — tenant kapsamsız

`oauth_clients` (§14.1). CLAUDE.md §6'nın tenant kapsamsız kuralı: R5 muafiyetiyle görünür, `tappa_app`'ten
açık `REVOKE ALL`, `ENABLE` **ve** `FORCE ROW LEVEL SECURITY`, `tenant_id` adlı sütun yok. `tappa_app` onu
**doğrudan okumaz**: onay sayfası, token ucu ve davet kuyruğu `resolve_oauth_client` çözümleyicisinden
(§14.3) yalnız görünen sütunları alır, secret hash'ini asla.

### 10.2 `op_*` — dört yazma, bir okuma (ADR 0021 sınıfı)

`tappa_opdefiner` sahipli, `op_touch_session(p_session)` ile başlar, aktör türetilir; yazmalar tek aşamalı
audit, okuma iki aşamalı bilet; dönüş `void`/`uuid`, durum kehaneti yok (ADR 0021 §2):

| Fonksiyon | Tür | Yazdığı | `operator_audit_log.kind` |
|---|---|---|---|
| `op_register_oauth_client(p_session, p_name, p_redirect_uris text[], p_scopes text[], p_secret_hash, p_secret_hint) → uuid` | yazma | yeni satır | `oauth_client_registered` |
| `op_rotate_oauth_client_secret(p_session, p_client uuid, p_secret_hash, p_secret_hint) → void` | yazma | secret (eskisi **anında** geçersiz) | `oauth_client_secret_rotated` |
| `op_set_oauth_client_redirects(p_session, p_client, p_redirect_uris) → void` | yazma | dönüş listesi | `oauth_client_redirects_changed` |
| `op_disable_oauth_client(p_session, p_client) → void` | yazma | `disabled_at` (**tek yönlü**) | `oauth_client_disabled` |
| `op_read_oauth_clients(p_session, p_ticket, …)` | okuma, iki aşamalı (`op_read_legal_versions` emsali, `00027:422`) | — | `read` |

- Secret Go'da (operatör yüzeyi) basılır, HMAC'lenir, **bir kez** gösterilir (*"göster"* yok — K-12'nin
  gerekçesi); rotasyon eskisini anında öldürür (tek istemci; çakışmalı rotasyon sonraki iş).
- **Dönüş URI kuralı (kayıtta) — Go'da VE SQL'de (savunma derinliği).** `https://`, port yok ya da 443, IP
  literali yok, userinfo yok, **sorgu yok**, parça yok, ≤ 5 adres, her biri ≤ 512 bayt (webhook URL
  kuralının — ADR 0028 §6 — aynısı). **Yalnız sandbox derlemesinde** ek olarak loopback biçimi (§10.4).
  `op_register_oauth_client` ve `op_set_oauth_client_redirects` her adresi gövdelerinde aynı kurala karşı
  sınar (sabit bir düzenli ifade: şema `https`, ardından küçük harfli DNS adı — sayısal son etiket ya da `[`
  yok —, isteğe bağlı `:443`, `/` ile başlayan yol, `?`, `#`, `@` yok; **ya da** loopback biçimi
  `^http://(localhost|127\.0\.0\.1):[1-9][0-9]{0,4}/` + aynı yol kuralı) ve geçmezse `22023` ile reddeder;
  tablo CHECK'i aynı ifadeyi taşır. **Loopback'i prod'da SQL'de reddetmek mümkün değil — ölçüldü:**
  veritabanında sandbox'ı ayırt eden bir işaret yoktur (migration'larda `sandbox` yalnız `00100`'ün bir
  yorumunda geçer; sanal UID kuralı da Go'dadır — ADR 0028 §11) ve aynı migration'lar iki derlemede koşar.
  Bir işaret eklemek (GUC, ayar satırı) yazılabilir yeni bir güven kökü olurdu. Yeterli olan: (i) operatör
  yüzeyinin derleme etiketli kapısı ve (ii) prod ikilisinin yetkilendirme ucunda **koşulsuz** `https` kuralı
  (V2) — `tappa_operator` DSN'ini tutan biri prod'da loopback bir satır yazsa bile o adrese kod gitmez; ve
  loopback adresi zaten yalnız onay veren kişinin kendi makinesine çıkar.
- `p_scopes` ⊆ kapalı küme — **`webhooks:manage` kümede değildir** (§13): fonksiyon ve şema CHECK'i onu
  reddeder. `p_name` 1–64 karakter, işletme adının izolasyon kuralı (`00093`).
- Kapanmış istemci yeniden açılmaz (yeni kayıt); kapanış bütün grant'ları etkisiz kılar ama tenant
  satırlarına yazmaz (§9).

### 10.3 Ekran — operatör yüzeyi

`ops.taptime.mt/operator/apps`: liste (ad, `client_id`, kapsamlar, dönüş adresleri, kayıt/kapanış anı),
*"Register"*, *"Rotate secret"*, *"Edit redirect URIs"*, *"Disable"*. Zincir operatör konsolununki (host
kapısı, güvenlik başlıkları, flood, same-origin, `requireOperator`, oturum bütçesi —
`internal/handler/operator/tenantapi.go:19-30`). Logo **ilk teslimde yok** (O-17): logo kopyalanabilir;
onay ekranının güveni **adın** ve **dönüş host'unun** yazılmasından gelir. Logo gerekirse `internal/brand`
kapısıyla (ADR 0024) sonraki iş.

### 10.4 İlk kayıt — Malta Calculator; yerel sandbox

| Ortam | Ad | Dönüş URI | Kapsamlar |
|---|---|---|---|
| prod (`taptime.mt`) | `Malta Calculator` | `https://maltacalculator.com/api/integrations/taptime/callback` | `timesheets:read employees:read employees:write` (A5) |
| yerel sandbox (`make build-sandbox`, `TAPPA_ENV=sandbox`) | `Malta Calculator (sandbox)` | Malta Calculator'ın yerel geliştirme callback'i, ör. `http://localhost:<port>/api/integrations/taptime/callback` — port Malta Calculator'dan öğrenilir | aynı |

- `www.` varyantı **kaydedilmez** (kullanıcı: *"yalnız istenirse"*; A6 *"if the site serves there"*); issue
  gövdesindeki `www.` adresi addendum ve kullanıcıyla düzeltildi. Tam eşleşme `www.`'yi kabul etmez.
- İzinli kapsamlar **istenenlerle sınırlı** (en az yetki). İzinli kapsamı sonradan değiştiren bir `op_*` v1'de
  yok; Malta Calculator ileride `punches:read` isterse ya o fonksiyon eklenir ya yeni kayıt açılır.

**Loopback callback — öneri: yalnız sandbox derlemesinde, tam eşleşmeyle.**

- Kabul edilen biçim: `http://localhost:<port>/<yol>` ya da `http://127.0.0.1:<port>/<yol>` — port
  **zorunlu ve kayıtlıyla aynı** (RFC 8252 §7.3'ün yerel uygulamalar için tanıdığı port esnekliği
  **verilmez**; bu bir sunucu istemcisi), sorgu/parça/userinfo yok.
- **İki kapı, ikisi de derleme etiketine bağlı:** (i) operatör yüzeyi kayıtta loopback'i yalnız
  `sandbox` etiketli ikilide kabul eder; (ii) yetkilendirme ucu V2'de prod ikilisinde `https://` dışını
  **her durumda** reddeder — `tappa_operator` DSN'ini tutan biri prod'da `op_register_oauth_client`'ı doğrudan
  loopback adresle çağırsa bile o adrese kod gitmez. Şema CHECK'i iki biçimi de kabul eder (aynı migration
  iki derlemede koşar); kural Go'dadır ve iki kapıda pinlenir (§17).
- Gerekçe: loopback adresi yalnız geliştiricinin kendi makinesine gider; sandbox'ın verisi sahtedir
  (ADR 0028 §11 — gerçek UID reddi, prod yedeği geri yüklenmez). Prod'da `https` + tam eşleşme değişmez.
- **Yerel sandbox kurulumu (runbook'a, uygulama turunda):** sandbox ikilisi `TAPPA_BASE_URL=http://localhost:8080`
  (varsayılan — `config.go:328`), ayrı `TAPPA_API_HOST` ve `TAPPA_OPERATOR_HOST`; operatör hesabı `cmd/opadmin
  create` ile; kayıt yerel operatör ekranından. Sertleştirilmiş derlemede çerezler `Secure`'dür
  (`cookie.go`'nun `IsHardened` dalı); `http://localhost` tarayıcılarda güvenli bağlam sayıldığından çalışması
  beklenir — **doğrulanmadı** (Safari özellikle), yerel kurulumda ölçülür. Yerel host adlarının
  `TAPPA_API_HOST`/`TAPPA_OPERATOR_HOST` kuralından (`isDNSHostName`) geçen biçimi de orada seçilir
  (doğrulanmadı).

## 11. Etkinleştirme, K-22 ve askı

### 11.1 Etkinleştirme (OA-1 = (a))

Onaylı (kayıtlı, etkin) bir uygulamaya bağlanmak işletmenin API'si için **operatör adımı istemez**: grant
kendi kapısıdır; **hiç açılmamış** bir K-22 grant'ı durdurmaz ve grant `tenants.api_enabled_at`'i **yazmaz**.
Operatörün **kapattığı** K-22 ise grant'tan üstündür (§11.4). Gerekçe:

- K-22'nin ölçülmüş gerekçesi **SSRF**'tir: `/signup` herkese açık olduğu için kapı olmadan herkes
  sunucumuza istediği adrese istek attırabilirdi (ADR 0028:110). Bir grant **adres girmez**: dönüş adresi
  operatörün kaydettiği bir listedir, webhook yoktur (§13).
- K-22'nin **güven** yarısı (bir operatörün bu dış erişimi onaylaması) OAuth'ta **uygulama kaydıyla**
  karşılanır: operatör uygulamayı bir kez, küresel olarak onaylar — issue'nun ikinci seçeneği.
- Anahtarlar ve owner'ın kendi webhook'u **K-22'ye bağlı kalır** — değişen yalnız grant türüdür (§11.3).
- Bedeli: API'si kapalı bir işletmenin verisi, owner'ın onayıyla, operatörün onayladığı bir uygulamaya
  akar; `companyCode` `null` olabilir. Operatör bir işletmenin OAuth'unu **K-22'yi kapatarak** durdurur —
  operatör kapanışı grant'tan üstündür (§11.4); istemciyi kapatmak bütün işletmeler için durdurur.

### 11.2 Askı (ADR 0025)

| Durum | Davranış | Gerekçe |
|---|---|---|
| Askıdaki işletmede **yeni onay** | onay sayfası *"suspended"* der; onay POST'u askı halkasında 403; **ret** askı halkasız zincirde → `error=access_denied` (§4.5) | yeni grant bir yazmadır; ret yazma değildir |
| Onaydan sonra, takastan önce askı | token ucunda `invalid_grant` (§5.2 adım 6) | grant doğumu yazmadır |
| Var olan tokenla `GET` | **açık** | ADR 0028 §10: okumalar askıyı bilmez |
| Var olan tokenla yazma | ADR 0028 §10 tablosu birebir | askı kontrol noktası tenant'tan okur |
| **Refresh** | **açık** | panel girişinin askıda açık olmasının karşılığı (ADR 0025 §4); kapalı olsaydı 1 sa sonra okumalar da biterdi |
| Panelden **Disconnect**, `/oauth/revoke` | **açık** | K-26'nın savunma yazmaları sınıfı → `protectWritingThroughSuspension`; ADR 0025 kapalı istisna listesine adıyla |
| Davet kuyruğu | ADR 0028 §10'a göre askıda açık (değişmez); kapısı §11.3 | — |

### 11.3 K-22'nin bütün okuyucuları ve OAuth'ta her biri (YÜKSEK-1)

**Sayım (ölçüldü — `grep -rn "api_enabled_at\|GetTenantAPIEnabledAt" internal db/queries cmd db/migrations`,
`_test.go` ve üretilmiş `internal/store` hariç).** Go'da altı yol, SQL'de dört fonksiyon ailesi:

| # | Okuyucu | Ne yapar | OAuth'ta |
|---|---|---|---|
| G1 | `internal/apikey/auth.go:194` (`Resolve`) | anahtar zinciri: kapalı → 403 `api_disabled` | **değişmez**; token zinciri onu çağırmaz — kendi çözümleyicisi operatör kapanışını ayrıca taşır (§8.1, §11.4) |
| G2 | `internal/apikey/keys.go:225` (`List`) | panelin *"Developers"*inde *"API kapalı"* durumu | **değişmez**; *"Connected apps"* bu bayrağa bağlı değildir (§12) |
| G3 | `internal/apikey/keys.go:316` (`Create`) | kapalıyken anahtar oluşturulamaz | **değişmez** — bir grant anahtar açmaz |
| G4 | `internal/webhook/endpoints/endpoints.go:269-285`, çağıranlar `:303` (liste), `:359`, `:415`, `:458`, `:499`, `:567` (`requireOpen`) | owner'ın uç noktasını oluşturma / adres / döndürme / ping / yeniden gönderme kapısı | **değişmez** — grant owner'ın webhook'unu açmaz (SSRF gerekçesi aynen durur) |
| G5 | `internal/webhook/delivery/delivery.go:339` | işçinin **gönderim anı** kapısı | **değişmez** — OAuth'ta webhook yok (§13) |
| G6 | `internal/invite/queue.go:292` | davet kuyruğu: kapalıysa **hiçbir şey basılmaz**, istek bekler (72 sa — `:82`) | **DEĞİŞİR** — aşağıda |
| G7 (yeni) | `resolve_oauth_token_by_hash` (§14.3) | — | OAuth `/v1`: operatörce kapalı → 403 `api_disabled` (§11.4) |
| S1 | `punch_feed()` (`00067:242`, `:255`) | olay **her durumda** yazılır; teslim satırı yalnız açıksa | **değişmez** — OAuth `punches:read` olay satırlarını okur (onlar K-22'den bağımsız yazılır); `employee_company`/`location_company` `api_code`'dan, `null` olabilir |
| S2 | `holding_feed()` (`00067:424-449`) | iki tarafın teslim satırı, her taraf kendi K-22'siyle | **değişmez** (aynı gerekçe; eş satırları OAuth'a zaten kapalı — §8.5) |
| S3 | `resolve_api_key_by_hash` (`00051:170`) | G1'in kaynağı | **değişmez**; token çözümleyicisi aynı `tenants` join'ini yapar ve `api_code`'u da okur (§14.3) |
| S4 | `op_enable_tenant_api` / `op_disable_tenant_api` (`00039:595`, `:658`; `00042:1001-1138`'in yeniden tanımı), `op_read_tenant_detail` (`00057`, `00063`) ve operatör ekranları (`internal/handler/operator/tenantapi.go:199`, `tenants.go:393-398`) | yazar ve gösterir | **değişmez**; operatör ekranı grant'ları göstermez (§9) |

`00043:153`'teki geçiş bir **yorumdur** (holding definer'ının matrisinde K-22'nin olmadığını söyler); okuma
değildir.

**G6 — sorun (denetimin YÜKSEK-1'i, okundu).** API'si açılmamış ama OAuth ile bağlı bir işletmede
`employees:write` ile e-postalı yeni çalışan `PUT`'u, aktive olmamış birinin adres değişikliği ve
`resend-activation` `invite_requests`'e satır yazar ve 2xx döner; kuyruk `queue.go:292-298`'de K-22'yi okur,
kapalı bulur, **hiçbir davet basmaz** ve istek 72 saat sonra ölür — Malta Calculator'ın eklediği çalışan
hiç davet almaz, kimse fark etmez.

**Seçilen uygulama.** Kuyruğun kapısı (revizyon 2: `employees:write`'a bağlı, operatör kapanışı üstün):

> **açık ⇔** `tenants.api_enabled_at IS NOT NULL`
> **VEYA** (işletmenin API'si **operatörce kapatılmamış** (§11.4: `api_code IS NULL`) **VE** işletmede,
> **etkin** bir istemciye ait, kapsamında **`employees:write`** bulunan en az bir canlı grant var).

- **Neden `employees:write`:** kuyruğa satır yazan üç yol (e-postalı yeni çalışan `PUT`, adres değişikliği,
  `resend-activation`) yalnız `employees:write` ile çağrılabilir (§8.2); yalnız `timesheets:read` taşıyan bir
  bağlantı işletmenin davetlerini açmamalı — kapı yetkiyle aynı kapsamı ister.
- **Neden tenant düzeyinde, isteği doğuran kimliğe göre değil (ölçüldü):** `invite_requests`'in birincil
  anahtarı `(tenant_id, employee_id)`dir (`00037:189`) — bir çalışanın tek isteği vardır ve onu bir anahtar
  da, bir grant da, sonraki bir istek de ezer; davet işletmenin **kendi çalışanına** gider, uygulamaya değil.
- **Nerede ve nasıl:** `queue.go:288-298`'deki tek okumanın yerine, **aynı işlemde** (`WithTenant(tenant)`),
  aynı sırada (ömür kontrolünden sonra, tavanlardan önce):
  1. `GetTenantAPIGate` (bugünkü `GetTenantAPIEnabledAt`'in `api_code`'u da döndüren biçimi, aynı satır):
     `api_enabled_at` doluysa kapı **açık**, bitti; `api_code` doluysa (operatör kapattı) kapı **kapalı**, bitti;
  2. ikisi de NULL ise yeni sorgu `ListLiveWritingOAuthClients`:
     ```sql
     SELECT client_id FROM oauth_grants
      WHERE tenant_id = @tenant_id AND revoked_at IS NULL
        AND 'employees:write' = ANY (scopes)
      ORDER BY client_id;
     ```
     **`LIMIT` yok:** kısmi UNIQUE `(tenant_id, client_id) WHERE revoked_at IS NULL` (§14.2) sayesinde satır
     sayısı işletmenin `employees:write`'lı bağlı uygulama sayısıdır, bu da operatörün kaydettiği istemci
     sayısıyla sınırlıdır (bugün 1). Sırasız bir `LIMIT` etkin bir istemciyi kaçırabilirdi (revizyon 1'in
     hatası — 2. tur DÜŞÜK-4);
  3. satırlar sırayla `resolve_oauth_client(client_id, NULL)` ile (§14.3, PK araması) çözülür; **ilk**
     `disabled_at IS NULL` olanda kapı açık, durulur. Hiçbiri etkin değilse kapalı.
- **Şema:** yeni tablo, fonksiyon ya da `tappa_app` yetkisi **yok**; iki sqlc sorgusu (birinin `tenants`'tan
  okuduğu ek sütun `api_code` — `tappa_app`'in `tenants`'ta tablo düzeyi SELECT'i var (`00001_create_tenants.sql:57`)
  ve `db/queries/directory.sql:18` onu bugün okuyor).
- **Neden tek ifade değil:** istemcinin `disabled_at`'i tenant kapsamsız `oauth_clients`'tadır ve
  `tappa_app`'in orada hiçbir yetkisi yoktur (CLAUDE.md §6, §10.1); tek ifade ya `tappa_app`'e o tabloda
  yetki (§6'ya aykırı) ya da tenant id alan yeni bir definer isterdi. Etkin-istemci süzgeci kapının **içinde**
  kalır (adım 3 açık kararı verir), kaçırma yoktur. İki adım, bugünkü okumanın kendi adlandırdığı yarışı
  taşır: *"The read takes no lock: a closing committed after it does not stop the one send already in
  flight"* (`queue.go:290-291`) — bir Disconnect, istemci kapanışı ya da operatör kapanışı uçuştaki **tek**
  gönderimi durdurmaz (değişmez).
- **Kapanışta bekleyen davetlerin kaderi:** **bugünkü K-22 kapanışıyla birebir** — istek **silinmez**,
  `closed` alır ve `queueClosedDelay` sonra yeniden vadeye girer; 72 saatlik ömrü içinde kapı yeniden açılırsa
  (yeni grant, operatörün API'yi açması) devam eder, açılmazsa `expired` yoluyla düşer (`queue.go:280-286`).
  Kapanışın kaynağı ne olursa olsun (grant iptali, istemci kapanışı, **operatörün K-22'yi kapatması**) aynıdır;
  operatörün kapanışı ayrıca grant'ın varlığını **ezer** (§11.4).
- **Panelin davetleri değişmez:** panelin davet yolu senkron ve K-22'siz; kuyruk yalnız API'nin isteklerini
  taşır (ADR 0028 §9, K-17).

### 11.4 Operatörün K-22 kapanışı grant'tan üstündür; istemci kapanışı

**Ölçüm — "hiç açılmamış" ile "operatör açtı, sonra kapattı" bugün ayırt edilebiliyor mu: evet.**

| Durum | `api_enabled_at` | `api_code` | Kanıt |
|---|---|---|---|
| hiç açılmamış | NULL | **NULL** | `api_code`'un tek yazarı `op_enable_tenant_api`'dir (`00039:595`, `00042:1001-1138`); `tappa_app` ona INSERT/UPDATE tutmaz (`00038`'in ön koşul bloğu); kayıt sihirbazı onu yazmaz |
| açık | dolu | dolu | CHECK `tenants_api_needs_a_code` (`00038:56-57`) |
| **operatör kapattı** | NULL | **dolu** | `op_disable_tenant_api` yalnız `api_enabled_at = NULL` yazar, `api_code`'u **korur** (`00039:690-692`; `db/queries/operator.sql:231` *"api_code is KEPT"*) ve tenant'a `tenant.api_disabled` audit satırı yazar (`00039:694-697`) |

Yani durum **şemadan** okunur, audit'ten değil: **operatörce kapalı ⇔ `api_enabled_at IS NULL AND api_code IS
NOT NULL`**. Yeni sütun ve migration **gerekmez**. Bu çıkarım `api_code`'un yalnız açarken yazılmasına
dayanır; o değişmez bir **katalog/yetki testiyle** pinlenir (`api_code`'a `op_enable_tenant_api` dışında
yazan yetki ya da fonksiyon yok) ve ileride *"kodu açmadan ata"* diye bir `op_*` doğarsa bu ADR'nin
değişikliğini ister.

**Operatörce kapalı bir işletmede (öneri — orkestratör kararı, kullanıcıya raporlandı):**

| Yüzey | Davranış |
|---|---|
| OAuth erişim tokenıyla `/v1` | **403 `api_disabled`** (anahtarla aynı kod — §8.1 adım 4); token çözümleyicisi bunu taşır (§14.3) |
| Yeni onay | onay sayfası *"Integrations are turned off for this business by Taptime"* der, yalnız *"Back to <App>"* (ret → `access_denied`) |
| Kod takası | `invalid_grant` |
| Refresh | **açık** — operatör yeniden açınca bağlantı yeniden onay istemeden döner; erişim tokenı yine 403 alır |
| Davet kuyruğu | **kapalı**, grant olsa da (§11.3) |
| *"Connected apps"* | bağlantılar görünür, *"Turned off by Taptime"* uyarısı; Disconnect açık |

Bedeli: hiç açılmamış bir işletmenin OAuth'unu durdurmak için operatör **kod atayıp açar, sonra kapatır**
(iki basış — operatör ekranı bunu *"Block integrations"* diye tek düğmeye bağlayabilir; ayrı iş). İşletme
başına ayrı bir *"OAuth kapalı"* sütunu bu yüzden gerekmiyor (§21'den çıktı).

**İstemci kapanışı (`op_disable_oauth_client`):** tenant satırına yazmaz (§9). Sonuçları:

| Yüzey | Davranış |
|---|---|
| o istemcinin erişim tokenları | çözümleyici `client_disabled_at`'i taşır → **401 `invalid_token`** (bir sonraki istekte) |
| token ucu / revoke | istemci kimliği reddedilir → **401 `invalid_client`** |
| yeni onay | V1 → yönlendirmesiz hata sayfası |
| davet kuyruğu | o istemcinin grant'ları kapıyı **açmaz** (§11.3 adım 3) |
| grant satırları | **iptal edilmez** (tenant yazması olurdu); *"Connected apps"* onları *"This app was turned off by Taptime"* durumuyla gösterir (ad çözümleyiciden, `disabled_at` dolu); owner Disconnect edebilir |

## 12. Panel — *"Connected apps"*

- **Yer:** *"Developers"* bölümünün (`/admin/api-keys`, owner'a özel — `adminview.go:413`) alt kartı
  `/admin/api-keys/apps`. Owner'a özel, okuma dahil; manager 403. API kapalı da olsa görünür (G2'ye bağlı
  değil).
- **Liste:** uygulama adı (çözümleyiciden), kapsamlar (insan diliyle), bağlanma anı ve onaylayan (*"by
  &lt;ad&gt;"*), son kullanım (dakika hassasiyeti). İki durum uyarısı (§11.4): istemci kapalıysa satırda
  *"This app was turned off by Taptime"*; işletmenin API'si operatörce kapatılmışsa listenin üstünde
  *"Integrations are turned off for this business by Taptime"*. İkisinde de Disconnect açık.
- **Disconnect** (POST, `protectWritingThroughSuspension` — §11.2): tek ifade
  `UPDATE oauth_grants SET revoked_at = COALESCE(revoked_at, clock_timestamp()), revoked_reason = 'owner',
  revoked_by = @actor WHERE id = @id AND tenant_id = @tenant_id AND revoked_at IS NULL` — tokenlar join'le
  ölür; iptal **tek yönlü** tetikleyiciyle (`00051`'in `api_keys_revocation_is_one_way` emsali). `audit_log`'a
  `oauth.grant_revoked` (aktör: üyelik).
- **Karar — grant işletmenindir, onaylayan kişinin değil (DÜŞÜK-7; orkestratör kararı (öneri), kullanıcıya
  raporlandı).** Onaylayan owner'ın üyeliği devre dışı kalırsa, manager yapılırsa ya da kişi işletmeden
  ayrılırsa grant **çalışmaya devam eder**. Gerekçe: bordro bağlantısı işletmenin bir sistemidir, bir kişinin
  değil; otomatik iptal bağlantıyı **sessizce** kırardı ve bunu görecek kişi devre dışı bırakmayı yapan owner
  değil, Malta Calculator'daki bordro sorumlusu olurdu. API anahtarları için kullanıcının T174 kararının
  aynısıdır (ADR 0028:3870-3906). Piyasa deseni: Slack'te bir yöneticinin kurduğu uygulama ve Google
  Workspace'te yöneticinin yetkilendirdiği uygulama, kurucu ayrıldığında **çalışmaya devam eder**; kurucu
  gösterilir, kaldırma yetkili kişinindir. Karşılığı: listede **her** owner bütün bağlantıları görür ve
  **koparabilir**; onaylayanın üyeliği devre dışıysa *"Approver disabled"* hapı (T174'ün *"Creator
  disabled"* hapı kalıbı). Durduran yollar: bir owner'ın Disconnect'i, istemcinin revoke'u, operatörün
  istemciyi kapatması, 30 sn sonrası refresh yeniden kullanımı.
- UI işi `tappa-brand` skill'iyle; tap/sonuç/aktivasyon ekranına dokunulmaz.

## 13. Grant başına webhook — kapsam dışı, izleyen iş (OA-3 = (a))

**Karar:** grant başına webhook aboneliği (`webhooks:manage`) bu ADR'nin teslimine **girmez**; ayrı bir iş
olarak izler (kendi ADR eki ve kartı).

- **Bu teslimde `webhooks:manage` kayıtta verilemez:** kapalı kapsam kümesinde **yoktur** (§8.2);
  `op_register_oauth_client` onu reddeder ve `oauth_clients.allowed_scopes` CHECK'i onu dışlar (§14.1);
  yetkilendirme isteğinde `invalid_scope` döner. Kapsamı açmak izleyen işin migration'ıdır.
- **Neden ayrı (kapsam/risk):** webhook bugün tenant başına **tek uç noktadır** (`00048:195`) ve teslim
  satırını `transactions`'a bağlı **iki tetikleyici** (`punch_feed`, `holding_feed`) **tek** uç noktaya yazar
  (`00067:289`, `:505`, `:519`); grant başına abonelik bu gövdelerin N uç noktaya yayılacak biçimde yeniden
  yazılması demektir — holding dokunuşunun sayaç kilidi (K-31, TF001), 55P03 yeniden denemesi ve askı
  orada. Uygulamanın girdiği adres yeni bir SSRF girdisidir; G4/G5'in K-22 kapısının grant karşılığı da o
  işte tanımlanır. Malta Calculator bu kapsamı istemiyor (A5).
- **İzleyen işin kabulü (öneri):** grant başına uç nokta ve imza anahtarı (`tt_whsec_`), adresin istemcinin
  kayıtlı host'larına bağlanması, mevcut teslim/yeniden deneme makinesi (API-9/API-10), §8.5'in eş kuralı
  (eş punch'ı grant'ın uç noktasına gitmez), Disconnect'in aboneliği aynı işlemde kapatması.
- Bedeli: issue'nun *"Per-grant webhook subscriptions"* kabul maddesi bu teslimde kapanmaz.

## 14. Şema taslağı (yalnız ad ve sütun — numara yok)

Her tablo ADR 0028 §1'in kuralıyla: `REVOKE ALL … FROM tappa_app` + yalnız gereken fiil ve **sütunlara**
`GRANT`; **INSERT ve UPDATE sütun listesiyle**; zaman damgaları `DEFAULT clock_timestamp()` ve INSERT
listesinde **yok** (ADR 0015 / ADR 0021 §2 bilet emsali — yazılabilir bir zaman sütunu tavanı anlamsızlaştırır);
her fonksiyonda `REVOKE ALL … FROM PUBLIC`; tetikleyici fonksiyonu `EXECUTE` yalnız sahipte ve `TG_RELID`'i
assert eder; katalog testine (`internal/db/privcatalog_test.go` ailesi) satır girer.

### 14.1 `oauth_clients` — tenant kapsamsız

| Sütun | Tip | Not |
|---|---|---|
| `id` | uuid PK `DEFAULT gen_random_uuid()` | `client_id` = `'app_' \|\| id` |
| `name` | text | 1–64, izolasyon kuralı |
| `redirect_uris` | text[] | 1–5; CHECK: her eleman §10.2'nin düzenli ifadesi (`https` biçimi **ya da** loopback biçimi), ≤ 512 bayt; `op_*` gövdeleri aynı ifadeyle sınar; loopback'in derleme kuralı Go'da (§10.2) |
| `allowed_scopes` | text[] | boş değil; CHECK `allowed_scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'` — **`webhooks:manage` yok** |
| `secret_hash` | text | `^[0-9a-f]{64}$`, UNIQUE |
| `secret_hint` | text | önek + 4 karakter (`prefix_hint` kalıbı) |
| `secret_rotated_at` | timestamptz | |
| `created_at` | timestamptz `DEFAULT clock_timestamp()` | yazılamaz |
| `created_by` | uuid | `platform_admins.id` |
| `disabled_at` | timestamptz | tek yönlü (tetikleyici) |

Yetki: `tappa_app` **hiçbir şey**; `tappa_opdefiner` INSERT/UPDATE sütun listesiyle; `tappa_resolver` SELECT
(`id`, `name`, `redirect_uris`, `allowed_scopes`, `secret_hash`, `disabled_at`) — hash okunur,
**döndürülmez**. RLS: `ENABLE` + `FORCE`.

### 14.2 Tenant tabloları (CLAUDE.md §6 beşlisi)

**Tek zaman kaynağı (2. tur DÜŞÜK-2).** Ömrü olan her satırda `created_at` ve `expires_at` **aynı okumadan**
gelir: ikisi de `now()` — PostgreSQL'de işlemin başlangıç anı, işlem boyunca **tek değer** — ve CHECK
**eşitlik**tir (`expires_at = created_at + <sabit aralık>`), üst sınır değil. Go saati hiçbir ömre girmez.
Seçim gerekçesi: (i) `clock_timestamp()` iki sütunda iki ayrı okuma olurdu ve eşitlik CHECK'i yazılamazdı;
(ii) tek okumayla dolduran bir tetikleyici aynı sonucu verir ama yeni bir tetikleyici fonksiyonu (sahip,
`EXECUTE`, `TG_RELID` kuralları) getirir — `now()` bunu bedavaya verir; (iii) `now()` işlemin başıdır,
yani ömür işlem süresi kadar **kısalır**, uzamaz — güvenli yön. Süre kontrolleri (`expires_at >
clock_timestamp()`, 30 sn toleransı) duvar saatiyle yapılır (ADR 0038: kaydın anı veritabanınındır).
Aralıklar **saat/saniye** cinsindendir (`interval '2160 hours'`, `'90 days'` değil): `timestamptz + gün`
oturumun saat dilimine (yaz saati) bağlıdır ve CHECK satırın **her** `UPDATE`'inde yeniden değerlendirilir —
başka saat dilimli bir oturumdaki `revoked_at` yazması eşitliği bozup reddedilirdi; saat aralığı bundan
bağımsızdır (PostgreSQL'in belgelediği davranış; bu ADR için ölçülmedi, uygulama turunda iki `TimeZone` ile
pinlenir).

**`oauth_codes`**

| Sütun | Not |
|---|---|
| `tenant_id`, `id` | `UNIQUE (id, tenant_id)` |
| `code_hash` | küresel UNIQUE, `^[0-9a-f]{64}$` |
| `client_id` | → `oauth_clients(id)` |
| `created_by` | → `admin_users(id, tenant_id)` |
| `redirect_uri`, `scopes text[]`, `code_challenge` (`^[A-Za-z0-9_-]{43}$`) | |
| `created_at` | `DEFAULT now()`, INSERT listesinde yok |
| `expires_at` | `DEFAULT now() + interval '60 seconds'`, INSERT listesinde yok; CHECK `expires_at = created_at + interval '60 seconds'` |
| `consumed_at` | CHECK `consumed_at IS NULL OR consumed_at >= created_at` |
| `grant_id` | → `oauth_grants(id, tenant_id)` |

`tappa_app`: `GRANT INSERT (tenant_id, code_hash, client_id, created_by, redirect_uri, scopes,
code_challenge)`; SELECT (`code_hash` hariç); `UPDATE (consumed_at, grant_id)`; DELETE yalnız kısıtlayıcı
politikayla (süresi bir günden eski — T130'un `webhook_deliveries_delete_settled` emsali).

**`oauth_grants`**

| Sütun | Not |
|---|---|
| `tenant_id`, `id` | `UNIQUE (id, tenant_id)` |
| `client_id` | → `oauth_clients(id)` |
| `scopes text[]` | boş değil, ⊆ kapalı küme |
| `created_by` | → `admin_users(id, tenant_id)` |
| `created_at` | `DEFAULT now()`, INSERT listesinde yok |
| `last_used_at`, `revoked_at` (tek yönlü), `revoked_reason` (kapalı küme: `owner`, `client`, `superseded`, `refresh_reuse`, `code_reuse`), `revoked_by` (üyelik, NULL olabilir) | CHECK `(revoked_at IS NULL) = (revoked_reason IS NULL)` |

Kısmi UNIQUE `(tenant_id, client_id) WHERE revoked_at IS NULL` (O-8). `tappa_app`: `GRANT INSERT (tenant_id,
client_id, scopes, created_by)`; SELECT; `UPDATE (last_used_at, revoked_at, revoked_reason, revoked_by)` — her
`revoked_at` yazması `COALESCE` + `WHERE revoked_at IS NULL` (00011 BOUNDARY 2); DELETE yok.

**`oauth_tokens`**

| Sütun | Not |
|---|---|
| `tenant_id`, `id` | |
| `grant_id` | → `oauth_grants(id, tenant_id)` |
| `kind` | `access` / `refresh` |
| `token_hash` | küresel UNIQUE, `^[0-9a-f]{64}$` |
| `scopes` | `text[]` — **erişim tokenında dolu, refresh'te NULL** (ORTA-3); CHECK `(kind = 'access') = (scopes IS NOT NULL)`, `scopes IS NULL OR cardinality(scopes) > 0` (boş küme yok), `scopes IS NULL OR scopes <@ '{timesheets:read,employees:read,employees:write,locations:write,punches:read}'`; grant'ın kapsamının alt kümesi olması Go'da, grant kilidi altında (dizi alt küme kontrolü bir yabancı tabloya CHECK'le bağlanamaz) |
| `created_at` | `DEFAULT now()`, INSERT listesinde yok |
| `expires_at` | INSERT ifadesinde **SQL'de** `now() + CASE @kind WHEN 'access' THEN interval '1 hour' ELSE interval '2160 hours' END` (parametre değil, sorgu metni); **CHECK:** `expires_at = created_at + CASE kind WHEN 'access' THEN interval '1 hour' WHEN 'refresh' THEN interval '2160 hours' END` — tek geçerli değer, Go başka bir anı yazamaz |
| `used_at` | CHECK `kind = 'refresh' OR used_at IS NULL`; `used_at IS NULL OR used_at >= created_at` |
| `revoked_at` | tek yönlü |

Soy sütunu (`parent_id`) **yok** — tolerans kuralı grant'ın bütün canlı tokenlarını iptal eder, soyu okumaz
(§5.3). `tappa_app`: `GRANT INSERT (tenant_id, grant_id, kind, token_hash, scopes, expires_at)`; SELECT (`token_hash`
hariç); `UPDATE (used_at, revoked_at)`; DELETE kısıtlayıcı politikayla (süresi bir saatten çok önce dolmuş
erişim tokenı; 90 günden eski kullanılmış/iptal edilmiş refresh). `expires_at` INSERT listesindedir çünkü
`DEFAULT` başka bir sütuna (`kind`) bakamaz; eşitlik CHECK'i onu tek değere bağlar. `kind`'e göre ayrı iki
tablo da aynı şeyi `DEFAULT`'la verirdi; tek tablo, çözümleyicinin tek UNIQUE'i (`token_hash`) için seçildi.

### 14.3 Çözümleyiciler — `tappa_resolver`, ADR 0002 md.7

| Fonksiyon | Girdi | Dönüş (taşı, uygulama) | ≤ 1 satır |
|---|---|---|---|
| `resolve_oauth_client(p_client_id uuid, p_secret_hash text)` | id, isteğe bağlı hash | `id, name, redirect_uris, allowed_scopes, disabled_at, secret_ok boolean` (`p_secret_hash` NULL → NULL) | PK |
| `resolve_oauth_code_by_hash(p_code_hash text)` | hash | `tenant_id, code_id, client_id, consumed_at, expires_at, grant_id` | `code_hash` UNIQUE |
| `resolve_oauth_token_by_hash(p_token_hash text)` | hash | `tenant_id, token_id, kind, grant_id, client_id, token_scopes` (**tokenın kendi** kapsamı — erişim tokenında dolu; `/v1`'in kapsam kontrolü bunu okur, grant'ınkini değil), `expires_at, used_at, token_revoked_at, grant_revoked_at, client_disabled_at, api_enabled_at, api_code` | `token_hash` UNIQUE; grant, istemci ve `tenants`'a PK join |

Hepsi `SECURITY DEFINER`, `STABLE`, `SET search_path = pg_catalog, pg_temp`, nitelikli adlar, dinamik SQL
yok, `tappa_app`'e yalnız `EXECUTE`, PUBLIC'ten `REVOKE`; `tappa_resolver`'a yalnız gereken **sütunlar**.

- **`tenants`'ta `tappa_resolver`'ın yetkisi bir sütun genişler:** bugün `SELECT (id, api_enabled_at)`
  (`00051:155`); token çözümleyicisi operatör kapanışını (§11.4) taşımak için `api_code`'u da okur →
  `SELECT (api_code)`. `api_code` sır değildir (punch'ta yayımlanır); patlama yarıçapı üç sütun. ADR 0002 M11
  notunun *"yalnız iki sütun"* cümlesine not düşülür.
- **`resolve_oauth_client` — ADR 0002 md.7 (i)'den bilinçli sapma.** md.7 (i) *"girdi yalnız hash"* der
  (ADR 0002 M11 notu); bu çözümleyicinin girdisi **açık bir kimliktir** (`client_id`, URL'de dolaşan genel
  bir değer). Gerekçe: (a) bir **tenant çözümlemesi değildir** — dönüşünde tenant yok; okuduğu tablo tenant
  verisi taşımaz (§9); (b) dönen sütunlar onay ekranının zaten herkese gösterdiği değerlerdir (ad, dönüş
  adresleri, kapsamlar, açık/kapalı) — sır ya da hash dönmez, secret eşleşmesi yalnız bir boolean'dır;
  (c) hash girdisi burada anlamsızdır: `client_id` gizli değildir. Emsal: `resolve_admin_owners_by_tenant`
  açık bir tenant id'si alır (ADR 0031 §5.2, M14-16b notu). Öteki dört kısıt aynen: ≤ 1 satır (PK), sabit
  sütunlar, `tappa_app`'in tabloda yetkisi yok, *"bağlam NULL iken göster"* dalı yok.
- ADR 0002'ye md.7 sayım notu (üç çözümleyici + bu sapma).

### 14.4 `operator_audit_log`

`kind` CHECK'i dört türle genişler (`00039:216-226` kalıbı: canlı tanımı bayt bayt karşılaştıran ön koşul
bloğu; okuma `read` türünü kullanır). Operatör `op_*` sahip testinin listesine **beş** fonksiyon (dört
yazma + bir okuma; ADR 0021 M11 notu).

### 14.5 `api_idempotency`

`key_id → api_keys` FK'si (`00052:54-55`) grant'ı dışlar. Öneri: `grant_id uuid NULL` (→
`oauth_grants(id, tenant_id)`), `key_id` NULL olabilir, `CHECK (num_nonnulls(key_id, grant_id) = 1)`, birincil
anahtar yerine iki kısmi UNIQUE (`(tenant_id, key_id, idem_key) WHERE key_id IS NOT NULL` ve grant'ınki).
Down: grant satırları silinir, eski PK geri gelir.

### 14.6 Migration sırası ve numara

1. `oauth_clients` + beş `op_*` + `operator_audit_log` türleri; 2. `oauth_grants`, `oauth_codes`,
`oauth_tokens` + tek yönlü iptal tetikleyicileri; 3. üç çözümleyici; 4. `api_idempotency` değişikliği.
Her biri geri alınabilir (`-- +goose Down` dolu; Down tablolarla birlikte bütün grant'ları öldürür — kapalı
yön, `00051`'in Down'u gibi). **Numaralar `main`'e birleştirme anında sıradaki boş numaralardır**; M16 dalı
`00104`'ten başladığı için bu dalda numara ayrılmaz. Rol eklemek gerekmez (`tappa_resolver`,
`tappa_opdefiner` var).

## 15. Kırmızı çizgi analizi (CLAUDE.md §4)

| # | Çizgi | Etki |
|---|---|---|
| 4.1 | Biyometri | **Yok.** Hiçbir kapsam yeni bir veri sınıfı açmaz; Taptime biyometri toplamaz. |
| 4.2 | GPS yalnız tap anında | **Değişmez.** `/v1`'in hiçbir yanıtı GPS, mesafe, IP taşımaz (ADR 0028 §6; #36 aynı dışlama). Arka plan konum isteyen kapsam ve rota yok. Token ucu ve onay istemci IP'sini **saklamaz**. |
| 4.3 | `transactions` değişmez | **Dokunulmaz.** Punch yazan rota yok; OAuth tabloları `transactions`'a bağlı değil, tetikleyici eklenmez (OA-3 ayrı işe bu yüzden). İptal tek yönlü; yalnız süresi dolmuş token satırı tembel silinir — kayıt değil, kimlik bilgisi. |
| 4.4 | Atomik `ctr` | **Dokunulmaz**; aynı ilke iki yeni yerde: kod tüketimi ve refresh döndürmesi **tek koşullu `UPDATE`**, 0 satır = red (§5.2, §5.3). |
| 4.5 | Tenant izolasyonu | §9 — grant tek tenant; tenant tabloları beşliyle; kayıt tenant verisi taşımaz; çözümleyici md.7; eşlerin verisi OAuth'ta hiçbir uçtan çıkmaz (§8.5); **yeni istisna yok**. |
| 4.6 | Kayıt kaybolmaz | **Dokunulmaz.** OAuth hiçbir kaydı reddetmez ya da süzmez; punch akışındaki eş süzgeci kayıt değil **görünürlük** süzgecidir (kayıt yazılmaya devam eder). |
| 4.7 | NTAG anahtarları | **Dokunulmaz.** Yeni sırlar AES anahtarı değildir; yalnız hash'leri saklanır, repoda ve log'da yer almaz; test vektörleri açıkça sahte. |

## 16. Tehdit modeli

| Tehdit | Karşılık |
|---|---|
| **Kod sızması** (tarayıcı geçmişi, istemcinin log'u, `Referer`) | 60 sn, tek kullanım; takas **client secret + PKCE verifier** ister; yeniden kullanım doğurduğu grant'ı iptal eder; onay sayfasında `Referrer-Policy: no-referrer` |
| **Redirect manipülasyonu / açık yönlendirme** | kayıtlı listeyle bayt bayt eşleşme; geçersiz istemci/adres → **yönlendirme yok**; kayıtta sorgu ve parça yasak; prod'da yalnız `https`; giriş dönüşü tek sabit yol (§4.2) |
| **CSRF / `state`** | `state` aynen döner; onay POST'u Origin kapısı + GATE-1 + form bağı + `rd`; `SameSite=Lax`; PKCE |
| **Bekleyen isteğin ezilmesi** (başka sekmede/saldırganın tetiklediği yetkilendirme) | `rd` — sayfanın çizdiği isteğin özeti çereze eşit değilse onay reddedilir (§4.2) |
| **Clickjacking** | `frame-ancestors 'none'` + `X-Frame-Options: DENY` |
| **Token sızması log'larda** | süreç log'u rota kalıbı; `/oauth` ingress access log'u kapalı; `Authorization` hemen silinir; yazdırılamayan tipler; R7/R7d; sabit `error_description` |
| **Refresh hırsızlığı** | gizli istemci: refresh tek başına işe yaramaz; 30 sn sonrası yeniden kullanım grant'ı iptal eder; 90 gün boşta ölür; Disconnect anında |
| **Client secret hırsızlığı** | rotasyon eskisini anında öldürür; istemciyi kapatmak bütün grant'ları bir sonraki istekte durdurur; secret tek başına grant vermez |
| **Confused deputy — yetkisi olmayan işletmeyi seçmek** | tenant formdan gelmez; seçim yalnız `identity_switch_session` üzerinden; rol işlemde yeniden okunur; form bağı; kod onaylayan üyeliğe bağlı, takasta yeniden denetlenir |
| **Holding eşinin verisinin üçüncü tarafa akması** | OAuth'ta eş süzgeci her uçta (§8.5) |
| **Cross-tenant token** | token → tek grant → tek tenant (bileşik FK); handler tenant'ı principal'dan alır; RLS + açık filtre |
| **Sandbox ↔ prod karışması** | ayrı önek DB'siz 401; ayrı veritabanı, HMAC anahtarı, kayıt; loopback callback yalnız sandbox derlemesinde, prod'da V2 `https` kapısı |
| **Oltalama — sahte "Connect Taptime"** | yalnız operatör kayıtlı istemciler; onay işletmeyi, uygulamayı ve **dönüş host'unu** adıyla yazar |
| **Kapsam yükseltme** | kapalı küme ⊆ istemcinin izinli kümesi ⊆ onaylanan; refresh'te genişletme `invalid_scope`; tabloda olmayan rota OAuth'a kapalı |
| **Mix-up** | RFC 9207 `iss` |
| **Kaba kuvvet / sel** | 256 bit entropi; adres kovası T90 kalıbıyla (`Reserve` → `Keep`/`Refund`): bir pencerede bir adresten çözümleyiciye ulaşan reddedilmiş kimlik **en çok 1200** (`/v1`) / **60** (token ucu), kesin; kimlik başına kovalar (§8.7) |
| **Eşzamanlı refresh** | grant satır kilidi + tek koşullu `UPDATE`; sonunda tek canlı çift (§5.3) |
| **Sessizce gitmeyen davet** (YÜKSEK-1) | kuyruk kapısı onaylı istemcinin canlı grant'ını tanır (§11.3) |

## 17. Test listesi

Issue'nun kabul maddeleri **kalın**; uygulama turunda adlar `Test<Paket>_…` biçiminde kesinleşir.

**Yetkilendirme**
- **redirect URI uyuşmazlığı** → yönlendirme yok (sondaki `/`, harf, `www.`, sorgu eklenmiş, port); bilinmeyen/kapalı `client_id` → yönlendirme yok.
- V3–V7 her biri kayıtlı adrese doğru `error` + `state` + `iss`; `state` aynen döner; 1024 bayt sınırı; tekrar eden parametre.
- bekleyen istek çerezi **her durumda** yazılır (oturumlu owner, manager, askı, oturumsuz); kurcalanmış / süresi dolmuş çerez → onay yok; giriş/seçim/geçiş dönüşü **tek sabit** yol.
- `rd`: A'nın sayfası açıkken B'nin isteği çerezi ezer → A'nın *"Allow"*u reddedilir, B onaylanmaz.
- onay POST'unda yeniden doğrulamanın on maddesi — her biri tek tek bozulunca red (operatörün arada adresi ya da kapsamı değiştirmesi dahil).
- çok işletmeli kimlik: başka owner üyelikleri listelenir; manager/askıdaki üyelikler listelenmez; geçiş sonrası onay doğru işletmeyi adlandırır.
- **form bağı:** KF'nin onay formu KM'ye geçmiş oturumla gönderilirse reddedilir.
- manager → `access_denied`; ret → `access_denied`; **askıdaki işletmede onay 403, ret `access_denied`** (DÜŞÜK-1); `TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException` ret rotasını adlı istisna olarak görür.
- çapraz siteden (`Sec-Fetch-Site: cross-site`) gelen `GET /admin/oauth/authorize` 200 + çerez (adlı istisna) — `mintGate`'li rotalar bugünkü gibi 403 (regresyon).
- başlıklar: `frame-ancestors 'none'`, `X-Frame-Options: DENY`, `no-store`, `no-referrer`; `form-action` yalnız `'self'` + **eşleşmiş** dönüş origin'i (başka kayıtlı adresin origin'i yok).
- **Tarayıcı ölçümü (elle, Chrome + Firefox + Safari):** onay POST'unun dış 303'ü (i) yalnız `'self'` ile, (ii) A planıyla — bloklanıyor mu; A planı bir tarayıcıda bloklanırsa B planı (§4.6) uygulanır ve aynı üç tarayıcıda yeniden ölçülür.
- prod derlemesinde loopback `redirect_uri` V2'de reddedilir (satır DB'de olsa bile); sandbox derlemesinde kabul edilir; operatör yüzeyi prod'da loopback kaydını reddeder.

**Token ucu**
- **PKCE uyuşmazlığı** → `invalid_grant` ve kod **tüketilmiş** (doğru verifier'la ikinci deneme de `invalid_grant`).
- **kod yeniden kullanımı** → `invalid_grant` + doğurduğu grant iptal; N goroutine aynı kod → tam 1 başarı (`-race`).
- süresi dolmuş kod (satırı geriye yazarak); başka istemcinin kodu; `redirect_uri` **yok** → kabul; `redirect_uri` farklı → `invalid_grant`.
- §5.1 tablosunun beş satırı (Basic + aynı/farklı gövde `client_id`, gövde `client_secret`, Basic yok).
- **refresh rotasyonu**: yeni çift; **yeniden kullanım** ≤ 30 sn → öteki canlı tokenlar iptal, yeni çift, R0 iptal **edilmez**; R0'ın pencere içinde **üçüncü** sunumu da başarılı; R0'ın ilk kullanımından > 30 sn sonra (tolerans yoluyla dokunulmuş olsa bile) → grant iptal + audit; N ≥ 3 goroutine aynı refresh → **N'nin N'si başarılı**, sonunda tam bir canlı refresh + bir canlı erişim tokenı; iptal edilmiş bir refresh → `invalid_grant`, grant canlı.
- **D (ORTA-2):** token isteğinin bağlamı commit'ten önce iptal edilirse (istemci bağlantıyı kapattı) ya da 10 sn aşılırsa işlem geri alınır — hiçbir token doğmaz, `used_at` yazılmaz; sıra bozulan senaryo (Rt önce, O sonra) D + kurala uyan istemciyle bağlantıyı koparmaz; ingress üzerinden istemci kopuşunun uygulamaya iptal olarak ulaştığı **canlı ortamda ölçülür** (doğrulanmadı).
- refresh `scope`: yok → grant kapsamı; alt küme (farklı sıra, tekrar) → o alt küme, `oauth_tokens.scopes`'a yazılır, yeni refresh grant kapsamında (refresh satırında `scopes` NULL); `scope=` boş → `invalid_scope`; genişletme → `invalid_scope`; daraltılmış erişim tokenı daraltmanın dışındaki rotada 403 `insufficient_scope` alır (grant o kapsamı taşısa bile); CHECK'ler: refresh satırına `scopes` yazmak, erişim satırını `scopes`'suz ya da boş kümeyle yazmak reddedilir.
- yanıt alanları ve başlıkları (A3); 429/503 gövdesi `temporarily_unavailable` + HTTP durumu + `Retry-After`.
- (tenant, istemci) başına tek canlı grant: ikinci onay birinciyi `superseded` yapar; eşzamanlı iki takas → biri `invalid_grant`.

**Revoke**
- refresh revoke → grant iptal, sonraki `/v1` 401; access revoke → yalnız o token; bilinmeyen / başka istemcinin tokenı → 200 ve hiçbir şey değişmez.

**`/v1`**
- **eksik kapsam → 403 `insufficient_scope`** + `WWW-Authenticate` + `details.scope`; tablonun her satırı (izinli 2xx, izinsiz 403) — tablo bazlı.
- yönlendiriciyi yürüyen test: kimlikli her rota kapsam tablosunda; tabloda olmayan rota OAuth'a 403; `webhooks:manage` istekte `invalid_scope`, kayıtta red.
- API anahtarı hiçbir rotada `insufficient_scope` almaz ve eşleri görmeye devam eder (regresyon).
- **eş süzgeci (ORTA-1):** holding'deki KF'nin OAuth tokenı — rehberde KM çalışanı yok; `punches?scope=employer`'da KF çalışanının KM plaketindeki punch'ı yok; `scope=location`'da KM çalışanının KF plaketindeki punch'ı yok; geçmiş sorgusunda (`from`/`to`) aynısı; imleç sayfalaması delikli akışta satır atlamaz. Aynı istekler KF'nin **anahtarıyla** eşleri döner.
- **cross-tenant token reddi:** A'nın tokenı B'nin `ref`'ini bulamaz (404), B'nin verisini görmez; RLS izolasyon testi her yeni tabloda (filtresiz sorguyla — CLAUDE.md §6).
- **iptal edilen grant bir sonraki istekte red** (Disconnect, revoke, operatörün istemciyi kapatması); secret rotasyonu tokenları **öldürmez** (ayrı test).
- süresi dolmuş erişim tokenı → 401 `error="invalid_token"`; `tt_at_test_` prod'da DB'siz 401 (çözümleyici çağrı sayısı 0).
- **oran (ORTA-A, kesin sınır):** tek adresten, tek pencerede, **eşzamanlı** 2 × 1200 çöp token biçimli istek → çözümleyici **tam olarak 1200** kez çağrılır, kalan 1200'ü 429 (sayaçlı sahte çözümleyiciyle; `-race`); aynı deney token ucunda 2 × 60 → **tam 60**; anahtar biçimli ve token biçimli çöpün **toplamı** 1200'ü geçmez (ortak limiter); tek adresten 5000 **başarılı** token isteği (çok grant'lı) adres kovasını tüketmez, grant (600) ve istemci (6000) kovasına sayılır; süresi dolmuş tokenla döngüye giren tek adres pencerenin kalanında 429 alır (adlandırılmış bedel); anahtar biçiminin adres kovası bugünkü gibi her istekte (regresyon); `TestLimiterAllowed_IsCalledByNoProductionCode` yeşil kalır.
- `GET /v1/business`: her kapsamla 200; `companyCode` `null` durumu; anahtarla da 200.
- K-25 bütçesi grant başına; audit satırı `actor_id = grant id`, `detail.via = "api"`, `detail.client_id`.
- Idempotency-Key grant'la çalışır; iki grant'ın aynı anahtarı çakışmaz.
- askı tablosu grant'la (ADR 0028 §10 hücreleri); refresh askıda açık.
- `Authorization` ve tokenın hiçbir parçası log'a/yanıta/audit'e geçmez (`TestChain_NothingOfTheKeyReachesALogOrAResponse` emsali, token için).

**Davet kuyruğu (YÜKSEK-1, 2. tur DÜŞÜK-4)**
- hiç açılmamış K-22 + etkin istemcinin `employees:write`'lı canlı grant'ı → davet basılır ve gönderilir; yalnız `timesheets:read`'li grant → `closed`; grant yok → `closed`; canlı grant ama istemci `disabled` → `closed`; iki istemci, ilki (sırada) kapalı ikincisi etkin → **açık** (kaçırma yok); K-22 açık → bugünkü yol (grant sorgusu koşmaz — çağrı sayısı 0); **operatör açıp kapattı** (`api_code` dolu, `api_enabled_at` NULL) + canlı grant → `closed`.
- Disconnect sonrası bekleyen istek silinmez, `closed` alır; 72 sa içinde yeni grant → gönderilir; ömür dolunca `expired`.
- e-postalı çalışan `PUT`, adres değişikliği ve `resend-activation` OAuth tokenıyla uçtan uca bir davet satırı doğurur (API'si hiç açılmamış işletmede).

**Operatör kapanışı ve istemci kapanışı (§11.4)**
- operatörce kapalı işletmede OAuth `/v1` → 403 `api_disabled`; refresh açık; yeni onay → `access_denied`; kod takası `invalid_grant`; yeniden açınca aynı token çalışır.
- katalog/yetki testi: `api_code`'a `op_enable_tenant_api` dışında yazan yetki ya da fonksiyon yok (§11.4'ün çıkarımının pini).
- istemci kapalı → erişim tokenı 401, token ucu 401 `invalid_client`, onay V1 hata sayfası, grant satırı iptal **edilmemiş**, *"Connected apps"* *"turned off by Taptime"* gösterir.

**Rota sınıfları ve zincirler**
- `TestAppRoutes_EveryNonReadRouteHasOneOfFourClasses`: `POST /oauth/token`, `POST /oauth/revoke` → sınıf 3; `POST /admin/oauth/authorize` → sınıf 1; `POST /admin/oauth/authorize/deny` ve `POST /admin/api-keys/apps/disconnect` → sınıf 2 (adlı istisna); `TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException` aynı ikisini adlı istisna görür.
- `GET /admin/oauth/authorize` oturumsuz: V1–V7 koşar, çerez yazılır, 303 `/admin/login` — `requireLogin` değil `onUnauthenticated`; V1 hatasında çerez yazılmaz.

**Şema / güvenlik**
- katalog testi: her yeni tablo ve fonksiyon matrisle (`tappa_app` `oauth_clients`'ta hiçbir şey; hash sütunlarında SELECT yok; **INSERT ve UPDATE sütun listesi**; zaman damgaları INSERT'te yok; PUBLIC `EXECUTE` yok).
- tek yönlü iptal tetikleyicileri (grant, token, istemci) — `COALESCE` korumalı toplu iptal yarışı.
- ömür CHECK'leri eşitliktir: kod 60 sn, erişim 1 sa, refresh 2160 sa — bir saniye eksik ya da fazla `expires_at` reddedilir; `created_at`/`expires_at` aynı işlemde `now()`'dan; iki ayrı `TimeZone`'lu oturumda (yaz saati sınırını aşan 90 gün) `revoked_at` yazması CHECK'e takılmaz; `allowed_scopes` CHECK'i `webhooks:manage`'ı reddeder.
- dönüş URI kuralı SQL'de: `op_register_oauth_client` / `op_set_oauth_client_redirects` `http://example.com/…`, IP literali, `:8443`, `?x=1`, `#f`, `user@` adreslerini `22023` ile reddeder; loopback biçimini kabul eder (derleme kuralı Go'da — prod operatör yüzeyi reddeder, prod V2 reddeder).
- `tappa_resolver`'ın `tenants` yetkisi tam olarak `(id, api_enabled_at, api_code)`.
- R5 muafiyeti `oauth_clients` için görünür; R7d yeni önekleri yakalar (kasıtlı ihlalde kırmızı); `pg-restore-verify.sh` ENABLE/FORCE sayısı.
- `TAPPA_OAUTH_HMAC_KEY` başka bir anahtara eşitse açılış reddedilir.
- `/oauth/*` API host'unda (`apihost.go:45`) ve operatör host'unda (`operatorhost.go:111-121`) 404; `/v1` ana host'ta 404 (regresyon).

## 18. Addendum'dan sapmalar

### 18.1 Uyum — addendum'un her satırı

| # | Addendum | Bu ADR | Dayanak |
|---|---|---|---|
| A1 | `{TAPTIME_OAUTH_BASE_URL}/oauth/authorize`, `/oauth/token`, `/oauth/revoke` (RFC 7009, form `token=…`) | üç uç aynı tabanda, `https://taptime.mt` (§3); revoke form `token=…` (§6) | RFC 7009 §2.1 |
| A2 | token isteği form-urlencoded, HTTP Basic, `code_verifier` (S256); refresh `grant_type=refresh_token` | §5.1, §5.2, §5.3 | RFC 6749 §2.3.1, §4.1.3, §6; RFC 7636 §4.5 |
| A3 | yanıt `access_token`, `token_type: "Bearer"`, `expires_in`, `refresh_token`, `scope` (boşlukla ayrık) | §5.4, alan alan | RFC 6749 §5.1 |
| A4 | hatalar `error` taşır (ör. `invalid_grant`); istemci `error_description`'ı göstermez | §5.4 — `error_description` sabit ve yalnız bilgi | RFC 6749 §5.2 |
| A5 | kapsamlar `timesheets:read employees:read employees:write` | kapalı kümede (§8.2), Malta Calculator kaydında (§10.4) | — |
| A6 | dönüş `https://maltacalculator.com/api/integrations/taptime/callback` (+ `www.` sunuyorsa) | yalnız apex kaydedilir (kullanıcı kararı) | §10.4 |
| A7 | `GET /v1/business` → `{id, name, companyCode}`, her kapsam okur | §8.4 — `companyCode` `null` olabilir (aşağıda 2) | — |
| A8 | ret → `error=access_denied` | §4.5 — askıdaki işletmede de | RFC 6749 §4.1.2.1 |

### 18.2 Sapmalar ve eklemeler (Malta Calculator'a issue yorumuyla)

**Uçlar ve akış**
1. `/oauth/authorize` 303 ile `/admin/oauth/authorize`'a yönlenir; tarayıcı izler, sizin URL'niz değişmez.
2. `/oauth/*` `https://taptime.mt`'de; `/v1` (ve `GET /v1/business`) `https://api.taptime.mt`'de — iki taban adres.
3. Yetkilendirme yanıtı ek olarak `iss=https://taptime.mt` taşır (RFC 9207).
4. Yetkilendirmede `redirect_uri` ve `scope` **zorunlu**; `state` ≤ 1024 bayt, yalnız VSCHAR (`%x20-7E`).
   Kayıtlı tek adres `https://maltacalculator.com/api/integrations/taptime/callback` (`www.` değil), bayt bayt.
5. Bir Taptime işletmesi bu uygulamaya **aynı anda tek bağlantı** taşır; yeniden bağlanmak (yeni onay + kod
   takası) öncekinin **bütün tokenlarını iptal eder**.

**Token ucu**
6. Token isteğinde `redirect_uri` isteğe bağlı; gönderirseniz yetkilendirmedekiyle bayt bayt aynı olmalı.
7. Basic + gövdede aynı `client_id` kabul; farklı `client_id` ya da gövdede `client_secret` → `invalid_request`.
8. Kod 60 sn yaşar ve **başarısız bir sunumda da tüketilir**; yanıtı kaybolan bir takası aynı kodla yeniden
   denemek bağlantıyı **iptal eder** — kod takasını tek uçuşta yapın, kaybolursa akışı baştan başlatın.
9. Refresh'te `scope` isteğe bağlı; verilirse boş olmayan, grant kapsamının alt kümesi (sıra önemsiz);
   genişletme ya da boş `scope=` → `invalid_scope`. Daraltılmış erişim tokenı yalnız o kapsamlara açıktır.
10. **Refresh kuralı:** aynı refresh tokenıyla **eşzamanlı istek yapmayın** (tek uçuş). Zaman aşımında
    bağlantıyı kapatın, **en az 10 sn bekleyin** (sunucu tavanı D) ve asıl isteği gönderdikten sonraki **30 sn
    içinde** aynı refresh tokenıyla yeniden deneyin: yeni bir çift alırsınız, öncekiler iptal edilir. 30 sn'den
    sonra eski token sunulursa **bağlantı kopar**, owner yeniden onaylamalı. Kurala uymayan eşzamanlı ya da
    sıra bozulmuş istekler de bağlantıyı koparabilir.
11. 90 gün kullanılmayan refresh tokenı ölür; owner yeniden onaylamalı.
12. 429 ve 503 → `{"error": "temporarily_unavailable"}` (429'da `Retry-After`) — RFC 6749 token ucu kümesinde yok;
    HTTP durumuna da bakın.

**Revoke**
13. Erişim tokenını revoke etmek bağlantıyı koparmaz; koparmak için **refresh tokenını** revoke edin.
14. Başka istemcinin tokenına da 200 döner (RFC 7009 §2.1 red öngörür).

**`/v1`**
15. `companyCode` `null` olabilir — işletmenin şirket kodunu Taptime operatörü API'sini açarken atar.
16. OAuth tokenıyla `/v1/directory` ve `/v1/punches` **holding eşlerinin** verisini döndürmez — API anahtarına
    göre davranış farkı.
17. Taptime bir işletmenin entegrasyonlarını kapatırsa `/v1` `403 api_disabled` döner; refresh çalışmaya devam
    eder, yeniden açılınca bağlantı onay istemeden döner.
18. Bilgi: tokenın kapsamında olmayan bir rota **403** `{"code": "insufficient_scope", …}` +
    `WWW-Authenticate: Bearer error="insufficient_scope", scope="…"` döner; süresi dolmuş/iptal edilmiş token
    **401** + `error="invalid_token"`.

**Malta Calculator'a sorular**
19. Yerel sandbox denemesi için yerel callback'inizin (`http://localhost:<port>/…`) portu.
20. Bir Taptime işletmesi birden çok Malta Calculator hesabına bağlanabilmeli mi? (Bugünkü tasarımda
    işletme başına tek bağlantı — madde 5; *evet* ise grant'ın tekilliği istemci hesabını da içermeli.)

## 19. Açık kararlar

**Kullanıcı kararı bekleyen yok** — OA-1 … OA-7 2026-11-14'te verildi (§0).

Kullanıcıya **raporlanan** orkestratör kararları (gerekçesi yazılı, itiraz gelirse değişir):
- DÜŞÜK-7 — grant işletmenindir, onaylayan kişinin değil (§12).
- ORTA-2 / ORTA-A — token biçimli istekte adres kovası T90 kalıbıyla (`Reserve` → `Keep`/`Refund`); grant
  600/dk, istemci 6000/dk; token ucunda adres 60/dk, istemci 600/dk (§8.7).
- YÜKSEK-1 — davet kuyruğu kapısı tenant düzeyinde *"K-22 açık veya (operatörce kapatılmamış ve etkin
  istemcinin `employees:write`'lı canlı grant'ı)"*; kapanışta bekleyen davetler K-22 kapanışıyla aynı kaderde (§11.3).
- Loopback callback yalnız sandbox derlemesinde, port dahil tam eşleşme (§10.4).
- Refresh toleransında grant'ın **bütün** canlı tokenlarının iptali ve yarışın bedeli (§5.3).
- Operatörün K-22 kapanışı grant'tan üstün; durum `api_code`'dan okunur, yeni sütun yok (§11.4).
- İstemci kapanışı grant'ları iptal etmez, etkisiz kılar (§11.4).

Bilgi bekleyen: Malta Calculator'ın yerel callback portu (§18.2 madde 19) ve bir işletmenin birden çok Malta
Calculator hesabına bağlanıp bağlanamayacağı (§18.2 madde 20 — *evet* ise O-8'in kısmi UNIQUE'i değişir).

## 20. Elenen seçenekler

| Seçenek | Neden elendi |
|---|---|
| JWT erişim tokenı | İptal önbelleksiz olmalı (ADR 0028 §3); JWT ya her istekte DB'ye bakar (kazancı yok) ya iptali 1 sa geciktirir |
| Public istemci (secret'sız) | İlk istemci sunucu taraflı; ihtiyaç doğunca kendi ADR'si |
| `client_secret_post` | Addendum Basic istiyor (A2); iki yöntem = iki doğrulama yolu |
| Panel çerezini `Path=/`'e genişletmek | Panel çerezinin tap ve diğer yüzeylere gitmemesi bilinçli (`cookie.go:30-49`); bir 303 yeter |
| Token ucunu API host'una koymak | Addendum'un tek taban adresi (A1); API host'unun *"yalnız `/v1`"* değişmezi |
| Onay formunda istek parametrelerini gizli alanla taşımak | Kurcalanabilir ve çerezle çift kaynak olurdu; çerez + `rd` tek kaynak (§4.2) |
| Grant'ı onayda (koddan önce) yaratmak | Takas edilmeyen onay listede hayalet bağlantı bırakır |
| Dinamik istemci kaydı (RFC 7591) | Herkese açık kayıt = oltalama yüzeyi |
| Refresh'te katı yeniden kullanım tespiti (OA-5 (a)) | Kullanıcı (b)'yi seçti; gizli istemcide kazancı düşük, bedeli sessiz kopma (§5.3) |
| Davet kuyruğu kapısını isteği doğuran kimliğe bağlamak | `invite_requests`'in PK'si çalışan başına tek satır (`00037:189`); şema değişikliği + *"son yazan"* belirsizliği (§11.3) |
| Kuyruk kapısını tek SQL ifadesi yapmak | `tappa_app`'e tenant kapsamsız tabloda yetki ya da tenant id alan yeni definer gerekirdi (§11.3) |
| Token biçimli istekte adres kovasını tamamen kaldırmak | Çöp tokenlarla veritabanına sınırsız çözümleyici çağrısı (§8.7) |
| *"Önce `Allowed`, başarısızlıkta `Charge`"* (revizyon 1'in biçimi) | T90'da ölçülüp kaldırılan iki adımlı yarış; eşzamanlı N istek sınırı N kez geçer; `Allowed` üretimde yasak (`limiterallowed_test.go:22`) — `Reserve`/`Keep`/`Refund` (§8.7) |
| Grant başına webhook'u bu teslime almak (OA-3 (b)) | Kullanıcı (a)'yı seçti; tetikleyicilerin yeniden yazımı (§13) |
| Logo URL'si (uzaktan) | Takip pikseli + CSP `img-src` genişlemesi |

## 21. Kapsam dışı (v1) ve izleyen işler

**İzleyen iş:** grant başına webhook aboneliği ve `webhooks:manage` kapsamı (§13).

**Kapsam dışı:** discovery metadata (RFC 8414) · token introspection (RFC 7662) · public istemci · kapsamı
onay ekranında daraltma · istemci logosu · operatörün bir istemcinin bağlı işletmelerini görmesi
(tenant-ötesi okuma) · operatör ekranında hiç açılmamış işletme için tek basışlık *"Block integrations"* (§11.4) · çakışmalı secret rotasyonu · istemci başına
izinli kapsamı düzenleme · OAuth ile punch yazma (API'de zaten yok) · canlı sandbox.

## 22. Uygulanınca değişecek belgeler

- **CLAUDE.md §3:** `internal/oauth/` satırı; §6'nın tenant kapsamsız tablo listesine `oauth_clients`
  (yazarı yalnız `op_*`); §7'nin asla loglanmayanlar listesine §7.4.
- **ADR 0002** md.7 sayım notu (üç çözümleyici; `resolve_oauth_client`'ın (i) sapması; `tappa_resolver`'a `tenants.api_code`) · **ADR 0021** M11 notunun `op_*` listesi (beş) ·
  **ADR 0025** kapalı istisna listesine ret rotası ve Disconnect · **ADR 0028** K-22 notu (grant türü; §11.3
  tablosu), K-23 notu (aktör), §9 davet kuyruğu kapısı, §10 (grant'a birebir).
- `cmd/tappa/approuter_test.go` sınıf kümeleri (§4.2) · `internal/handler`'ın askı rota testi.
- `openapi.yaml` (`GET /v1/business`, `insufficient_scope`, OAuth güvenlik şeması, eş süzgecinin açıklaması)
  ve rehberin iki dili (*"Apps connecting with OAuth"*; *"OAuth scope"* ile punch `scope`'unun ad ayrımı;
  token ucunun hata biçimi ve §18.2'nin maddeleri) — birlikte değişir (ADR 0030).

## Uygulama notları (3. tur denetimi)

ADR'yi **bloklamayan**, uygulama turunun gerçek testleriyle doğrulanacak notlar:

- **Atıf (düzeltildi):** `op_disable_tenant_api`'nin `UPDATE`'i `00039:690-692`, audit satırı `:694-697` —
  §11.4'teki atıflar buna çevrildi (önceki sürüm bir satır kaymıştı).
- **Idempotency:** `apiidempotency.sql:50`'nin `ON CONFLICT`'i PK'den kısmi UNIQUE'e geçince
  `WHERE key_id IS NOT NULL` yüklemini taşımalı (yoksa çıkarım indeksi bulamaz); `:31-41`'in tembel temizliğinin
  grant karşılığı yazılmalı (§14.5).
- **Operatörce kapalı işletme kontrolü** (§11.4) §4.2'nin onay POST'u listesinde ve §5.2 adım 6'da da **açıkça**
  bir madde olmalı (bugün yalnız §11.4 tablosunda).
- **V7 hatasında** (`state` biçimsiz/uzun) geçersiz `state` yönlendirmeye **yansıtılmaz**; yanıt `state`'siz döner.
- **Aynı kodla N goroutine:** beklenen sonuç pinlenir — **tam olarak 1** başarı. Karar (öneri): eşzamanlı ilk
  sunumlar **yeniden kullanım sayılmaz**, yalnız **tüketilmiş ve grant doğurmuş** bir koda **sonradan** gelen
  sunum yeniden kullanımdır ve grant'ı iptal eder (§5.2 adım 4). Dikkat: §5.2 adım 3'ün düz `UPDATE`'inde
  kaybeden, kazananın satır kilidini **bekler**, kazanan commit edince yüklemi yeniden değerlendirip 0 satır
  görür ve o anda `grant_id` artık doludur — yani düz biçimde eşzamanlı kaybeden **yeniden kullanım** sayılır
  ve yeni grant'ı öldürürdü. Ayrım bu yüzden kilitte yapılır: tüketim
  `UPDATE oauth_codes … WHERE id = (SELECT id FROM oauth_codes WHERE id = @code_id AND tenant_id = @tenant_id
  FOR UPDATE SKIP LOCKED) AND consumed_at IS NULL AND expires_at > clock_timestamp()` biçimindedir; satır
  kilitliyse (eşzamanlı bir takas sürüyor) kaybeden **beklemeden** 0 satır alır ve yalnız `invalid_grant`
  döner, grant'a dokunmaz; kilit serbestken 0 satır alan sunum ise ardışıktır ve adım 4'e girer. Test iki dalı
  ayrı pinler (N eşzamanlı → 1 başarı, grant canlı; commit sonrası tek sunum → grant iptal).
- **Ölü grant** (canlı refresh tokenı kalmamış — 90 gün boşta ya da yarışta kaybedilmiş): *"Connected apps"*te
  *"Expired — reconnect from the app"* görünür; davet kuyruğu kapısı (§11.3) ölü grant'ı **saymaz** — kapı
  sorgusu *"en az bir canlı refresh tokenı olan grant"* ister.
- **§11.4 pin testi** `prosrc` taramasıyla yapılmalı: `tappa_opdefiner` `UPDATE (api_code)` tutar
  (`00039:260`), yani yetki testi tek başına yetmez — `api_code`'a yazan gövdeler `op_enable_tenant_api`
  dışında olmamalı.
- **CSP eklemesi** (§4.6, `form-action` + dönüş origin'i) ret POST'unu taşıyan **manager**, **askı** ve
  **operatörce kapalı** sayfalarına da uygulanmalı — oralardan da dış adrese 303 edilir.
- **Double-clickjacking:** *"Allow"* düğmesi sayfa görünür olduktan sonra kısa bir gecikmeyle (ör. ~500 ms,
  `visibilitychange`/odak sonrası) etkinleşir; §16'nın clickjacking satırına eklenir. Panel CSP'si betik
  yüklemez (`adminlogin.go:2182-2185`) — gecikme betiksiz (CSS) ya da adlı bir betik istisnasıyla yapılır;
  seçim uygulama turunda.
- **`insufficient_scope`** §18.2'ye bilgi maddesi olarak girdi (madde 18).
- **E-postayla doğrulanmış kayıt oturumu** (yeni işletme kaydının sonunda açılan oturum) `continue` dönüşünü
  **bilinçli olarak** taşımaz: yeni kayıt OAuth akışının ortasında beklenen bir yol değildir; kişi kayıttan
  sonra uygulamadan *"Connect"*e yeniden basar.
- **`expires_in`** yanıtta `expires_at - now()`'dan (saniye, aşağı yuvarlanmış) hesaplanır, sabit 3600 yazılmaz.


**1. faz (veri katmanı) uygulamasının notları (2026-11-14):**

- **Migration sırası §14.6'dan farklıdır (bilinçli).** Dört dosya: `00104` tablolar (`oauth_clients` +
  üç tenant tablosu + tek yönlü tetikleyiciler), `00105` üç çözümleyici, `00106` `api_idempotency`,
  `00107` kayıt tablosunun operatör yarısı (`tappa_opdefiner` yetkileri, `platform_admins` FK'si, dört
  audit türü, `oauth_clients` okuma türü, `op_begin_read`/`op_read_audit` değişimi, beş `op_*`).
  Gerekçe: `internal/db`'nin `opAtVersion`'ı operatör nesnesi adı geçen her migration'ın Down'ını test
  işlemi içinde yürütür; `00104`–`00106` operatör nesnesi taşımaz ve `opAtVersionSkips`'tedir, böylece
  operatör testleri yalnız tablo düşürmeyen `00107` Down'ını yürütür. ADR'nin gerçekten dayandığı tek
  sıra (tablolar fonksiyonlardan önce) korunur. Numaralar `main`'e birleştirmede yeniden verilebilir.
- **Kod tüketimi tek ifadede sınıflandırır.** `ConsumeOAuthCode` (`db/queries/oauth.sql`) yukarıdaki
  `… WHERE id = (SELECT … FOR UPDATE SKIP LOCKED) AND consumed_at IS NULL AND expires_at > clock_timestamp()`
  biçimini taşır ve her zaman bir satır döner: `reached=false` (satır kilitli — eşzamanlı takas sürüyor;
  yalnız `invalid_grant`, grant'a dokunulmaz), `spent` (bu sunum tüketti), `reached ∧ ¬spent` +
  `prior_grant_id` (ardışık sunum → §5.2 adım 4, grant iptali). "Tüket, sonra bak" iki ifade olsaydı
  kazanan commit ettikten sonra bakan bir kaybeden yeni grant'ı iptal ederdi — ayrım kilitte, tüketen
  ifadenin içinde yapılır.
- **Kabul edilen sınır — süresi dolmuş refresh satırının silinmesi.** `oauth_tokens_delete_expired`
  bir refresh satırını doğumundan 2160 saat sonra (süresi dolunca) silinebilir kılar. Sonuç: süresi
  dolmuş, kullanılmış bir refresh tokenı silindikten sonra yeniden sunulursa **bilinmeyen token** sayılır
  (`invalid_grant`) ve grant **iptal edilmez** (§5.3 adım 4'ün yeniden kullanım tespiti çalışmaz). Kabul
  edildi: 90 gün boşta kalmış bağlantı zaten ölüdür (canlı refresh tokenı yoktur, *"Expired — reconnect"*),
  yeniden kullanılan token da ölü bir tokendır; tespitin koruyacağı canlı bir şey kalmamıştır.

**2. faz (yetkilendirme sunucusu) uygulamasının notları (2026-11-14, `ea0ba641`; 3. fazda yazıldı):**

- **Ön kapının sınırı `oauthapi`'dedir:** `GET /oauth/authorize` panel zincirinin dışında kendi adres
  tavanıyla koşar — adres başına **3000 / 10 dk** (panelin sel tavanının sayıları); 303 eder, çerez
  okumaz/yazmaz, DB'ye gitmez (`internal/handler/oauthapi/surface.go`'nun `frontLimit`/`frontPeriod`'u).
- **Ek audit eylemleri:** ardışık kod yeniden kullanımı grant'ı iptal ederken `oauth.code_reuse_detected`
  yazar (§5.2 adım 4); bir onayın yerine geçtiği (`superseded`) grant için `oauth.grant_revoked`
  (`reason: superseded`); `oauth.grant_created`'in detayı yerine geçilen grant id'lerini
  `superseded` dizisinde taşır.
- **Manager, askıdaki ve operatörce kapatılmış işletmenin onay POST'u** istemciye `error=access_denied`
  ile döner (yazma yok, kod yok); askı halkası askıdaki işletmede 403'ü zaten verir, alan katmanı
  (`oauth.Server.Approve`) üçünü ayrıca reddeder.
- **Double-clickjacking:** *"Allow"* `disabled` çizilir; `web/static/js/oauthconsent.js` (ayrı dosya,
  `script-src 'self'`, satır içi betik yok) onu sayfa **görünür ve odaklıyken 600 ms** sonra etkinleştirir;
  `blur` ve görünürlük kaybı kapatır, geri gelince bekleme baştan başlar. Betiksiz tarayıcı `<noscript>`
  içindeki ikiz formu (aynı bağ, aynı alanlar, etkin) görür — **gecikme yoktur**, adlandırılmış sınır.
  CSS ile gecikme seçilmedi: ortak stylesheet'e `@keyframes` girmez (panel CSS'inin kuralı) ve
  animasyonla düğme etkinleştirilemez. 3. faz: `arm()` `document.hasFocus()` da ister (yorumdaki
  *"visible and focused"* ile tutarlı).
- **Secret rotasyonunda gösterim kuralı:** yeni secret yalnız rotasyondan sonraki okuma rotasyonun
  tuttuğunu gösteriyorsa (canlı uygulama, ipucu yeni secret'ınki) **bir kez** gösterilir; uygulama
  kapalıysa *"No secret was changed"*, okuma başka bir şey gösteriyorsa *"The secret may have been
  changed… Rotate it again"* — `op_*`'ın kendi yanıtı durum söylemez (ADR 0021 §2 v 7). Disable tek
  yönlüdür ve askının onay kutusu kalıbıyla açık onay ister.
- **`response_type` (V3):** yok → `invalid_request` (eksik zorunlu parametre); var ve `code` değil →
  `unsupported_response_type` (RFC 6749 §4.1.2.1).
- **Bekleyen istek çerezinin boyutu:** çerez adıyla birlikte en çok **3800 bayt**
  (`MaxRequestCookieBytes`); içerik **HTML kaçışsız** JSON'la kodlanır (`state`'teki `<` 6 değil 1 bayt).
  En kötü kabul edilen durum — beş kapsam, 512 baytlık dönüş adresi, JSON'un ikiye katladığı `"`'dan
  1024 baytlık `state` — **ölçüldü: 3796 bayt** (pay 4 bayt; `TestOAuthRequest_TheCookieHasASizeBound`).
  Kuralın genişlemesi `ErrRequestTooLarge` ile reddedilir, tarayıcının düşürebileceği bir çerez yazılmaz.
- **`/oauth` log'suz Ingress yolundadır** (`deploy/k8s/40-ingress.yaml`, panelin `/admin` kalıbı). Kabul
  edilen bedeller (3. fazda yazıldı): controller'ın **hata** log'u upstream hata/zaman aşımında istek
  satırını sorgusuyla (`state` dahil) yazabilir; eski yönlendirme host'ları (`www.taptime.mt`,
  `tappa.everva.com.tr` — `43-redirect-ingress.yaml`) access log'u açık tutar, oraya gelen bir
  `/oauth/authorize?state=…` loglanır (yalnız eski host'la yapılandırılmış bir istemcide).
- **CSP `form-action` tarayıcıda ÖLÇÜLMEDİ.** A planı (`'self'` + eşleşmiş dönüş origin'i) uygulandı;
  §4.6'nın Chrome/Firefox/Safari ölçümü yapılmadı — `main`'e birleştirmeden önce devir (B planı hazır
  değil, gerekirse o ölçümün sonucuna göre).

**3. faz (/v1 bearer, kapsam, eş süzgeci, kapılar, "Connected apps") uygulamasının notları (2026-11-14):**

- **Zincir (§8.1) `internal/handler/api/chain.go`'dadır, `internal/apikey`'de değil.** Biçim ayrımı
  `apikey.ParseBearer` → önce `oauth.Server.CheckAccess` (DB'siz `tt_at_<env>_`+43), değilse anahtar yolu
  bugünkü gibi. Token yolu: ortak adres limiter'ında `Reserve` → `ResolveAccess` → 401/403'te `Keep`,
  kabulde `Refund` → grant (600/dk) ve istemci (6000/dk) kovası → `TouchGrant` → K-19 → `scopeGate`.
  `apikey.Principal` genelleşti (`KeyID` | `GrantID`+`ClientID`+`Scopes`; `IsGrant`, `Full`, `OwnOnly`,
  `HasScope`, `ActorID`); ADR'nin `Kind`/`Full`/`OwnOnly` alanları yöntem olarak kuruldu (tek kaynak:
  `GrantID`'nin doluluğu).
- **Süre kontrolü veritabanının saatiyle (sapma değil, ek sorgu).** Çözümleyici `expires_at`'i taşır;
  Go saatiyle karşılaştırmak §14.2'nin "tek saat" kuralına ikinci kaynak olurdu. Bu yüzden
  `ResolveAccess` grant'ın tenant'ında tek ifadeli `CheckOAuthAccessTokenLive`'ı
  (`expires_at > clock_timestamp()`) koşar — token istekleri istek başına bir ek kısa işlem öder.
  401 sırası: bilinmiyor / `kind≠access` / token, grant iptal / istemci kapalı / süresi dolmuş;
  ardından operatörce kapalı → 403 (401, 403'ten üstün — anahtarın kuralı).
- **Kapsam tablosu tek yerde (`internal/handler/api/scope.go`, `routeScopes`)**, kapı yönlendirmeden
  sonra chi'nin `RoutePattern`'ını okur; tabloda olmayan kimlikli rota (sandbox'ın `POST
  /v1/sandbox/taps`'i, test rotaları) token'a **403 `insufficient_scope`** (`details` boş,
  `WWW-Authenticate: Bearer …, error="insufficient_scope"`, `scope` parametresi yok). Envanter:
  `TestScopes_EveryAuthenticatedRouteIsInTheTable` gönderilen yönlendiriciyi yürür.
- **401 gövdesi:** kod `unauthorized` (kapalı küme değişmedi), mesaj token'a özgü sabit cümle, başlık
  `Bearer realm="taptime", error="invalid_token"`. Biçimsiz değer (iki şekle de uymayan) bugünkü düz
  401'dir (`error` parametresiz) — bilinçli: hangi türün denendiği bilinmiyor ve anahtar istemcilerinin
  bugünkü yanıtı değişmemeli (RFC 6750 §3.1 burada `invalid_request` önerebilirdi; seçilmedi).
- **`GET /v1/business` ve `insufficient_scope` OpenAPI'ye ve rehbere girdi (4. faza taşma, testlerin
  zorunluluğu):** `TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8`, `…EveryRouteAndErrorDocumented`
  ve `TestGuide_EveryEndpointCodeAndLimitIsTheDocuments` yönlendiricinin her rotasını, kapalı kod
  kümesini ve `ErrorDetails` anahtarlarını belgeye bağlar; bu yüzden asgari kayıt yazıldı (yol + `Business`
  şeması + `scope` ayrıntı anahtarı + iki rehberde §2.3 ve tablo satırları). OAuth güvenlik şeması, her
  işlemin `x-error-codes`'ına `insufficient_scope` (prob'ları OAuth token'ıyla yazılmalı) ve rehberin
  *"Apps connecting with OAuth"* bölümü **4. fazındır**.
- **Eş süzgeci:** rehberde `DirectoryQuery.OwnOnly` → `holding_directory` **çağrılmaz**; punch akışı ve
  geçmiş sorgusu `own_only` parametresiyle `roles = '{employer,location}'` (§8.5'in ifadesi birebir).
  Anahtar `OwnOnly=false` — davranış değişmedi. `timesheets` değişmedi (zaten yalnız kendi çalışanları).
- **Audit atfı:** API yazmalarının detay yapıları (`staffapi.go`, `venueapi.go`, bütçe uyarısı) grant'ta
  `client_id` (`app_<uuid>`) taşır, anahtarda alan yoktur (`omitempty`); `actor_id` = grant id. K-25 kilidi
  ve sayımı `ActorID` ile — sorgular değişmedi. `ListAPIBudgetAlerts` `oauth_grants`'a LEFT JOIN'le
  `grant_client_id` döner; ad `apikey.Keys.NameAppsWith(*db.DB)` ile Go'da, pencerede görünen istemci
  başına bir kez, işlemden sonra çözülür.
- **Idempotency:** `apikey.Writes.Run` principal türüne göre `…ForGrant` ikizlerini kullanır; tembel
  süpürme iki ad alanını zaten birlikte taşıyordu (1. faz).
- **Davet kuyruğu kapısı (§11.3):** `invite.Queue.gate`; `NewQueue` dördüncü zorunlu bağımlılık
  (`AppResolver`, üretimde `*db.DB`) alır. Sıra: `GetTenantAPIGate` (açık → açık, sorgu yok; `api_code`
  dolu + `api_enabled_at` NULL → kapalı) → `ListLiveWritingOAuthClients` → ilk etkin istemci.
- **Panel "Connected apps":** `GET /admin/api-keys/apps` (okuma zinciri, owner'a özel — yönetici 403) ve
  `POST /admin/api-keys/apps/disconnect` (`protectWritingThroughSuspension`, sınıf 2, ADR 0025'in adlı
  istisna listesi — K-26). API anahtarları sayfası karta bağlantı verir. Disconnect `oauth.grant_revoked`
  yazar: aktör üyelik, `detail.reason = owner`, `detail.via = panel`. Ret yanıtları (`unknown-app`,
  yetkisiz) yazma yapmaz; yöneticinin reddedilen POST'u bugünkü `api_key.refused` satırını yazar
  (`act: disconnect-app`).
- **2. fazdan devreden DÜŞÜK'ler kapandı:** tolerans dalının *"revoked N"* log satırı yalnız commit
  edilen yolda yazılır (geniş kapsamlı tolerans isteği `invalid_scope` alır, iptaller geri alınır —
  test); R7'ye `v.State`/`r.State`/`p.Req.State`, çerezin `c.Value`'su ve `p.sealed` dar adlarla eklendi
  (ağaçta önce sayıldı: 0 isabet; çıplak `.State` ve `sealed` yanlış pozitif verirdi — kontrol vektörleri);
  operatör ana sayfası `/operator/apps`'e bağlanır (test adı `…FourLinks…` korundu, içeriği beş bağlantı).
- **Önceden kırık bir test paketi düzeltildi (kapsam dışı, gerekçeli):** `internal/operatorauth`'un dört
  harici testi 2. fazın `operator.New` imza değişikliğinden sonra **derlenmiyordu**; reddeden bir
  `surfApps` ve sahte bir hasher eklendi.

**4. faz (denetim DÜŞÜK'leri, belgeler, yerel sandbox) uygulamasının notları (2026-11-15):**

- **`Principal.Full` kapalı düşer:** tam yetki `KeyID` dolu **ve** `GrantID` boşken verilir; ikisi de boş
  bir principal (sıfır değer, kimlik döndürmeyen bir çözümleyici) hiçbir rotada tam değildir ve eşleri
  dışarıda bırakır (`OwnOnly = !Full`). Zincir, `ResolveAccess`'in kabul edip grant id'si döndürmediği
  bir tokenı principal'a hiç çevirmez: **503** (bizim arızamız; rezervasyon iade). Testler
  `TestPrincipal_FullFailsClosed`, `TestOAuthChain_AnAcceptedTokenOfNoGrantIsUnavailable`.
- **Ölçülen açık, kapatıldı — `X-Request-Id`'de OAuth değerleri:** istek kimliği süzgeci
  (`internal/httpx/requestlog.go`) yalnız `tt_live_`, `tt_test_`, `tt_whsec_`'i reddediyordu; 54
  karakterlik bir erişim tokenı (`tt_at_live_…`) kümenin karakterlerinden, sınırın içinde ve iki önekin
  hiçbirini içermediği için **olduğu gibi log'a yazılıyordu** (ölçüldü: yeni satırlar düzeltmeden önce
  kırmızı). Süzgeç dört öneki (`tt_cs_`, `tt_ac_`, `tt_at_`, `tt_rt_`) de reddeder; rehber §2.2 ikisini
  birlikte söyler (`TestGuide_TheRequestIDRuleIsTheLogs`). §7.4'ün *"asla loglanmaz"* kuralının uygulaması.
- **OpenAPI:** `oauth` güvenlik şeması (Authorization Code; üç URL Taptime'ın kendi host'unda —
  `servers`'ın host'u eksi `api.`); her kimlikli işlemin `security`'si *"anahtar VEYA rotanın kapsamlı
  tokenı"* (`GET /v1/business`: `oauth: []`); kapsamlı dokuz işlemin `x-error-codes`'unda
  `insufficient_scope` — prob'ları OAuth tokenıyla (`TestOpenAPI_EveryRouteAndErrorDocumented`), belge
  ile kapsam tablosu birbirine `TestOpenAPI_TheOAuthSchemeIsTheScopeTable` ile bağlı. `x-limits`'e
  `requestsPerGrantPerMinute` (600) ve `requestsPerAppPerMinute` (6000) girdi (kod sabitlerine pinli,
  rehber §3.5'te). `GET /v1/business`'in ikinci örneği (`connected`, `companyCode: null`) hikâyede bir
  erişim tokenının gerçek yanıtıdır. Rehberin *"adres yok"* denetimi (`TestGuide_NoSecretAddressOrPersonalData`)
  OpenAPI belgesinde yalnız bu OAuth host'una izin verir. Sandbox belgesinde `POST /v1/sandbox/taps`
  `insufficient_scope`'u listeler (kapsam tablosunda yok — §8.2).
- **Rehber:** yeni §13 *"Apps connecting with OAuth"* / *"OAuth ile bağlanan uygulamalar"* (11 alt bölüm);
  *"Changes to this guide"* §14 oldu; baskı 5. Örnek token yanıtı yerel sandbox'ın gerçek yanıtının
  biçimidir: `expires_in` **3599** (§5.4'ün uygulama notu: `expires_at - now()`, aşağı yuvarlanmış) ve
  `scope` kanonik (alfabetik) sırayla — istekteki sırayla değil.
- **Yerel sandbox tarifi (§8.6, §10.4):** `make sandbox-local-oauth` (operatör ekranından loopback
  dönüşlü test uygulaması → sahip onayı → kod takası Basic + PKCE → `/v1/business`, `/v1/timesheets`
  seed haftası, `POST /v1/sandbox/taps` 403 → refresh → revoke → 401/`invalid_grant`) ve
  `make sandbox-local-app` (yalnız kayıt; kimlik bilgileri yalnız verilen dosyaya). Kayıt `op_*`'a
  operatör yüzeyinin kendi formundan gider (doğrudan tablo yazması yok). `deploy/README.md` *"Sandbox"*
  §10. Ölçüldü 2026-11-15, `tappa_sbx_oauth1`.
- **Sapma (orkestratörün brief'i, §10.4'ün tablosundan):** yerel sandbox'a Malta Calculator **`Malta
  Calculator`** adıyla ve **prod** dönüş adresiyle (`https://maltacalculator.com/api/integrations/taptime/callback`)
  kaydedildi — tablo `Malta Calculator (sandbox)` ve Malta Calculator'ın yerel callback'ini önerir. Yerel
  port hâlâ bilinmiyor (§18.2 madde 19); yerel geliştirmede o adres *"Edit redirect URIs"*le eklenir.
  Kapsamlar A5 (`timesheets:read employees:read employees:write`).
- **`client_id` biçimi:** `app_` + UUID (§7.1) — `tt_` önekli değildir (sır değildir).
- **CLAUDE.md §6:** tenant kapsamsız tablo listesine `oauth_clients` eklendi (§22).
- **Açık kalanlar (bu fazda yapılmadı):** §4.6'nın tarayıcı ölçümü
  (`form-action`, Chrome/Firefox/Safari); ingress'in istemci kopuşunu uygulamaya iptal olarak iletmesi
  (§5.3 (ii), canlıda).
- **Düzeltme turu (2026-11-15, doğruluk denetiminin ORTA'sı ve DÜŞÜK'leri):** rehber §2.1 *"API'yi Taptime
  açar"* OAuth istisnasını söyler (bağlanmak bu adımı istemez; Taptime'ın kapattığı API'de token da 403
  `api_disabled`), §3.1'in `unauthorized` satırı erişim tokenını da anar; §13.3 yönlendirmesiz hata
  sayfasının durumlarını adıyla sayar (ayrıştırılamayan sorgu; eksik, tekrarlanan, biçimsiz, bilinmeyen,
  kapatılmış `client_id`; eksik, tekrarlanan, kayıtsız `redirect_uri`); §13.4 düğmeleri (*"Don't allow"*,
  öteki durumlarda *"Back to"*), işletme listesini (owner olunan, askıda olmayan) ve bekleyen isteğin 10
  dakikasını; §13.5 ve §13.11 kod takasının ek `invalid_grant` gerekçelerini (§5.2 adım 6, §11.4); §13.9
  loopback'i (yalnız ad ve IPv4 adresi — IPv6 değil —, yalnız sandbox); §13.3 ve §13.6 verifier'ın ve
  refresh tokenının yalnız sunucuda tutulmasını. Yerel sandbox betiği env dosyasını kaynak olarak
  çalıştırmaz (dört değeri okur, loopback değillerse reddeder), canlı kimlik bilgisi dosyalarını bir `EXIT`
  tuzağıyla siler ve `SANDBOX_APP_OUT`'u deponun ağacında reddeder.
- **`make check` turu (2026-11-15):** OAuth'un derleme ayrımı kendi build-constraint'li dosyalarında
  (`internal/oauth/build_prod.go`, `build_sandbox.go`) duruyordu; ürün kodunda constraint yalnız
  `internal/config`'in iki derleme dosyasına ve `cmd/tappa`'nın iki dosyasına izinlidir
  (`internal/db`'nin `TestProductCode_CarriesNoBuildConstraint`). İki dosya kalktı: `oauth.Env` ve
  `oauth.LoopbackRedirects` artık `config.CredentialWord` ve `config.IsSandboxBuild`'den gelen **derleme
  zamanı sabitleri** (`internal/oauth/build.go`) — prod ikilisinde hiçbir yapılandırma ya da değişken
  loopback'i açamaz. Etiketsiz derlemenin değerleri atlamasız bir testle pinli
  (`TestBuild_ProductionIsLiveAndRefusesLoopback`; ölçüldü: `IsSandboxBuild` true yapılınca
  `TestOAuthRedirect_ProductionV2RefusesLoopback` atlayıp yeşil kalıyordu). Onaylayanın düşürülmesi
  testi ham `UPDATE admin_users` yerine panelin kendi `SetAdminMemberRole`'unu kullanır
  (`cmd/opadmin`'in tablo kilidi envanteri); `internal/oauth` ve `internal/handler/oauthapi` o
  envanterde *"outside, plain"* olarak adlı.
