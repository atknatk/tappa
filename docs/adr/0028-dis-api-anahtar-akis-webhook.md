# ADR 0028 — Dış entegrasyon API'si: API anahtarı, imleçli punch akışı, webhook ve sandbox

- **Durum:** kabul edildi — **uygulama: yok (M11).** Bu ADR yazıldığında (HEAD `bcbff6d`, dal
  `m11-api`) Tappa'da dış dünyaya açık bir JSON API'si, Bearer kimliği, API anahtarı tablosu,
  webhook, OpenAPI belgesi, Idempotency-Key, `punch_events`/`feed_counters` tabloları ve dial-time
  bir SSRF kapısı **yoktur**; dışarıya giden tek HTTP çağrısı VIES'tir ve egress açıktır (plan §1).
  Uygulama görevleri: GATE-1, API-1 … API-13 ([m11-entegrasyon-api.md](../plan/m11-entegrasyon-api.md) §5).
- **Tarih:** 2026-10-11. Plan 2026-10-09/10'da on bir denetim turundan geçti; 1.–5. turlar bu ADR'nin
  holding dışı çekirdeğini (olay tetikleyicisi, sayaç satırı imleci, `arwd` kuralı, SSRF izin listesi,
  Idempotency, pasifleştirme bütçesi) ölçerek kurdu (4. tur üçüncü göz ONAY, 5. tur güvenlik ONAY —
  `690c69b`); 11. sürüm (`480df19`) iki mercekten **ONAY**, onaylı plan `bcbff6d`.
- **Karar veren:** kullanıcı (ürün sahibi) — **K-2** (2026-10-09: aktivasyon dokunuşu punch değildir,
  ADR 0026 kalır), **K-3** (2026-10-10: sandbox `taptime.mt` dışında **ayrı alan adı** + **ayrı
  derleme**), **K-19** (2026-10-09: etkinlikle kurulan işçi, tenant'lar arası tarama yok), **K-25**
  (2026-10-10: API pasifleştirmesi anında + anahtar başına saatlik bütçe), **K-26** (2026-10-10:
  askıda savunma yazmaları açık). Otonom kararlar (öneri uygulandı — gerekçeli tablo aşağıda): K-4 …
  K-18, K-20 … K-24, K-28, K-31/K-32'nin olay yarısı, K-37.
- **Etkilenen (M11'de doğacak):** goose migration'ları (API-1, API-2, API-3, API-4) ·
  `scripts/db-init/01-roles.sql` (`tappa_feedwriter`) · `internal/config` (`IsHardened`, yeni env
  anahtarları, `TAPPA_API_HOST`) · `internal/db/pool.go` (rol reddi) · `internal/db/resolve.go`
  (yedinci çözümleyici) · `internal/httpx` (host kapısı, iki katlı oran sınırı) · yeni paketler
  `internal/apikey`, `internal/handler/api`, `internal/webhook`, `internal/egress`, `internal/sandbox`
  (§13) · `internal/invite` (davet kuyruğu işçisi) · panel yazma kapıları (GATE-1) ·
  `scripts/redline-check.sh` (R4, R7, R7b, R7d) · `scripts/pg-restore-verify.sh` ·
  `cmd/tappa/scriptguards_test.go` · CLAUDE.md §3, §6, §7
- **İlgili:** [ADR 0027](0027-holding-ve-capraz-tenant-dokunusu.md) (holding — iki hedefli olaylar,
  holding tetikleyicisi, rehber) · [ADR 0002](0002-tenant-baglami-ve-rls.md) md.7 (yedinci
  çözümleyici) · [ADR 0003](0003-sdm-modu-ve-anahtar-yonetimi.md) (`sun.Seal`) ·
  [ADR 0009](0009-onay-karari-geri-alinamaz.md) · [ADR 0010](0010-deaktivasyon-tek-yonlu.md)
  (K-25 sapması) · [ADR 0011](0011-duzeltme-satiri-yalnizca-kisaltir.md) ·
  [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) / [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md)
  (K-22'nin `op_*`'ı) · [ADR 0022](0022-islemsel-eposta.md) §7, §9 (davet) ·
  [ADR 0025](0025-tenant-askiya-alma.md) (askı) · [ADR 0026](0026-aktivasyon-ilk-nfc-dokunusunda.md) ·
  CLAUDE.md §4.3, §4.4, §4.5, §4.6, §4.7, §6, §7

**Atıf kuralı.** *"Plan §N"* = [m11-entegrasyon-api.md](../plan/m11-entegrasyon-api.md)'nin
bölümü; *"spec §N"* = kullanıcının paylaştığı *"TapTime API — Kurulum Rehberi (KF-RMS ve KM-ERP
entegrasyonu)"* (2026-10-09) — **repoya konmadı**: depo public ve belge müşterinin iç webhook
adresini taşıyor. Kırmızı çizgiler hep *"CLAUDE.md §4.x"*; çıplak *"§N"* bu ADR'nin bölümüdür.

## Tek cümlede

Tenant'ın panelinden ürettiği **API anahtarıyla** dış İK/ERP sistemi (KF-RMS → KF, KM-ERP → KM)
yalnız **kendi tenant'ına** lokasyon ve çalışan yazar, punch'ları **veritabanı tetikleyicisinin
yazdığı**, tenant başına **sayaç satırıyla sıralanan** ve **hiçbir satırı atlamayan** bir imleçli
akıştan çeker ve **webhook** ile anında alır; giden her istek **dial anında** gerçekten bağlanılan
adrese uygulanan bir **izin listesinden** geçer; ve bunların hiçbiri, operatör o tenant için API
erişimini açmadıkça (K-22) çalışmaz.

## Neden bir ADR

CLAUDE.md §10. Bu iş dört şeyi birden getirir: yeni bir **güvenlik sınırı** (Bearer kimliği, ayrı
host, yedinci çözümleyici), yeni bir **giden ağ yüzeyi** (müşterinin girdiği adrese sunucudan istek
— SSRF), yeni bir **sıralama sözleşmesi** (dışarıya verilen ve kaybolmaması gereken imleç) ve
`transactions`'a bağlı yeni **tetikleyiciler** ile yeni bir definer rolü (`tappa_feedwriter`).
Holding ile ilgili her şey (tenant'lar arası dokunuş, `holding_*` fonksiyonları, holding
tetikleyicisi) ADR 0027'dedir; bu ADR yalnız onun **olay yarısını** (iki hedefli punch, K-31'in
sayaç kilidi) burada sözleşmeye bağlar.

## Bağlam — bugün (ölçüldü, plan §1)

| Olgu | Kaynak |
|---|---|
| JSON API, Bearer, API anahtarı, OpenAPI, Idempotency-Key yok | plan §1 |
| `transactions`'ta monoton bir anahtar yok (dışarıya verilebilecek imleç yok) | plan §1 |
| Dışarıya giden tek HTTP VIES; DB outbox yok; dial-time SSRF koruması yok; **egress açık** (NetworkPolicy yalnız gelen trafiği sınırlar) | plan §1, §6 |
| `httpx.Limiter` bellekte (`replicas: 1`); 429'da `Retry-After` var | `internal/httpx/ratelimit.go` |
| Davet senkron; tavanlar: tenant 50/saat 300/gün · çalışan 3/saat · kutu 5/saat 20/gün · süreç kesicisi 300/saat; EM-5B canlıda | ADR 0022 §7, §9 ve EM-7C notu |
| Çalışan: tek `full_name`, external id/kod yok, `updated_at` yok, `location_id NOT NULL`; e-posta tekilliği `employees_tenant_email_key (tenant_id, email) WHERE email IS NOT NULL` — **pasif satırlar dahil**; kod çakışmayı **kısıt adıyla** tanıyor | `db/migrations/00003_create_employees_sessions.sql:83`, `internal/domain/tenant/staff.go:706-717` |
| Lokasyon: external id yok, aktif bayrağı yok, saat dilimi **tenant** sütunu | `db/queries/tenants.sql:158-164` |
| Dev dokunuş simülatörü dört katlı kapının arkasında | `internal/config/config.go:731-745`; ADR 0026 *"Geliştirme aracı"* |
| Panel yazma kapıları `Origin` yokken `Sec-Fetch-Site: same-site`'ı kabul ediyor (dört `sameOrigin` kopyası) | `internal/handler/adminlogin.go:1604-1614`, `signup.go:814-824`, `adminreset.go:1500-1510`, `activate.go:1601-1616` |
| Üretim kodunda **dokuz** `IsProd()` çağrısı | `config.go:326`, `config.go:1000`, `db/pool.go:119`, `session/cookie.go:136`, `adminauth/cookie.go:109`, `handler/cookies.go:270`, `signupstate.go:440`, `logincontext.go:297`, `activate.go:184` (yeniden sayıldı, 2026-10-11) |
| Üretimde `pg_default_acl` = `tappa_app=arwd/tappa_owner`; taze CI'da `ar` — CI fazla yetkiyi **göremez** | plan §4.0; ADR 0021 *"Bağlam"* |
| Veritabanında altı çözümleyici (`resolve_tag_by_uid`, `resolve_session_by_token_hash`, `resolve_invite_by_code_hash`, `resolve_admin_by_email`, `resolve_admin_session_by_token_hash`, `resolve_password_reset_by_token_hash`); `tappa_resolver`'ın `tenants` üzerinde yetkisi **yok** | `db/migrations/*`, `internal/db/resolve.go` |

## Karar

### Kullanıcı kararları

| # | Soru | Karar (kullanıcı, ürün sahibi) | Bedeli — adlandırılmış |
|---|---|---|---|
| **K-2** | Aktivasyon dokunuşu punch mı? | **ADR 0026 kalır** — aktivasyon dokunuşu `transactions` satırı yazmaz, punch değildir; ilk punch ikinci dokunuştur | Sözleşme farkı (plan §2 madde 1); spec'in *"ilk dokunuş = ilk punch"* beklentisi karşılanmaz |
| **K-3** | Sandbox | **Ayrı alan adı** (`taptime.mt` dışı) + **ayrı derleme** (2026-10-10). Alt alan + çerez geçişi denendi (6. sürüm, *"COOKIE-1"*), maliyeti büyüdü, geri dönüldü. Panel yazma kapılarının sertleşmesi (GATE-1, K-37) yine yapılır | Alan adı + DNS kullanıcıda; ayrı imaj (§11) |
| **K-19** | Arka plan işçisi tenant'lar arası tarama yapsın mı? | **Etkinlikle kurulan işçi — tarama YOK**; CLAUDE.md §4.5 bu yüzden değişmez | Sunucu yeniden başlayınca (her deploy) bekleyen webhook denemeleri ve kuyruktaki davetler tenant'ın bir sonraki dokunuşuna ya da API çağrısına kadar bekler (plan §2 madde 20); imleçli akış etkilenmez |
| **K-25** | API'den pasifleştirme ADR 0010'un iki adımlı onayını atlar | **Anında + anahtar başına saatlik bütçe** | ADR 0010'a sapma notu; aşımda 429 + owner uyarısı + audit (§12) |
| **K-26** | Askıda savunma yazmaları | **Açık:** anahtar iptali ve uç nokta devre dışı bırakma | ADR 0025'e not; askının geri kalanı ADR 0025'e birebir (§10) |

### Otonom kararlar (öneri uygulandı — gerekçeli)

| # | Karar | Gerekçe | Yeri |
|---|---|---|---|
| K-4 | Akışa giren punch = `verdict='ok'` ∧ kanal ∈ {`nfc`,`qr`} ∧ ¬`practice`, **veya** aynı kanal/practice şartını sağlayan **onaylanmış** `flag`. `reject`, `ignored`, reddedilmiş flag, manuel, practice **asla** | Spec §3.5 (*"doğrulanamayan dokunuş punch'a dönüşmez"*); CLAUDE.md §4.6 Tappa içinde karşılanır (kayıt yine yazılır, yalnız akışa girmez). Dev DB'de `practice=true` ∧ `flag` satırı var (4693, ölçüldü) → onay tetikleyicisi de tam şartı uygular | §4 |
| K-5 | v1'de iptal yolu yok → `version=1`, `status='valid'` | ADR 0011 (düzeltme yalnız kısaltır), ADR 0009 (onay geri alınamaz) — geçerli bir kaydı iptal eden yol yok | §4 |
| K-6 | Manuel ve tarihsel practice satırlar akışa girmez | Spec §9: manuel punch'lar RMS/ERP'de; çift sayımı önler | §4 |
| K-7 | Lokasyon başına saat dilimi sütunu **yok**; farklı `timezone` → 422. Holding üyeleri aynı saat dilimi (ADR 0027 K-36) | Raporlar tenant saat dilimiyle (CLAUDE.md §6) | §9 |
| K-8 | Dış kimlikler: `emp_` / `loc_` / `pch_` / `dlv_` + UUID; `tag_` + uid | Önek tip karışıklığını yakalar | §8 |
| K-9 | Ayrı host `api.taptime.mt` (`TAPPA_API_HOST`), **iki yönlü** host kapısı | Panel çerezleri API host'una gitmez; HTML rotaları orada 404, `/v1` ana host'ta 404 (bugünkü `operatorHostOnly` tek host bildiği için genelleşir) | §13 |
| K-10 | Anahtar: `tt_live_` öneki + 43 karakter base64url (sandbox'ta `tt_test_`); depoda kendi env anahtarıyla HMAC-SHA256; prod yalnız `tt_live_`, sandbox yalnız `tt_test_`; yanlış önek DB'ye gitmeden 401 | Deponun mevcut jeton kalıbı; önek sızıntı taramasını (R7d) mümkün kılar | §3 |
| K-11 | İmza spec biçiminde: `sha256=` öneki + gövdenin hex HMAC-SHA256'sı; ek zaman damgası başlığı **yok** | İmzaya girmeyen bir başlık sahte güven verir | §6 |
| K-12 | Uç nokta adresi ve imza anahtarı **panelden** (owner); anahtar yalnız oluşturma ve döndürmede **bir kez** gösterilir, *"göster"* yok | Mühürlü değer geri açılabildiği için *"göster"*, ele geçirilmiş bir oturuma sahte imzalı punch yollatırdı | §6 |
| K-13 | İmleç = **hedef tenant başına sayaç satırı** (`feed_counters`); küresel dizi ve advisory kilit yok | 2. tur: küresel dizi `CACHE 1` değilse kilit altında bile 1234 olayın 1016'sı kaçıyordu ve tenant'lar arası hacim sızdırıyordu; 3. tur: sayaç satırıyla 0 kaçan, iki mutasyon kırmızı | §5 |
| K-14 | `employees.location_id` API'den yönetilen satırlarda NULL olabilir — `CHECK (location_id IS NOT NULL OR external_ref IS NOT NULL)`; okuyucular API-1'de sayılır | Spec'te ev lokasyonu yok. Bedeli: böyle bir çalışanda *"farklı şube"* notu düşmez (plan §2 madde 24; `internal/domain/tap/decide.go` ev lokasyonuyla karşılaştırır) | §9 |
| K-15 | E-posta değişince bekleyen davetler **aynı işlemde** emekliye ayrılır + audit (ADR 0022 §7'nin panel kuralı); çalışan aktive değilse yeni davet kuyruğa; aktive çalışana davet gitmez | Eski kutudaki kodun çalışanı devralmasını önler | §9 |
| K-16 | API'den yeni çalışan = otomatik davet | Spec §4.2; panel davranışı değişmez | §9 |
| K-17 | Davet kuyruğu `invite_requests`; kod **gönderim anında** basılır; kira 60 sn; `resend-activation` istek anında hiçbir daveti öldürmez; **bugünkü tenant tavanları** (panel ve kuyruk aynı sayaçları paylaşır) | 2. tur: istek anında emekliye ayırma resend döngüsünü çalışanı süresiz kodsuz bırakan bir araca çeviriyordu; ayrı tenant'lar → KM, KF'nin tavanını tüketemez (EM-7C dersi) | §9 |
| K-18 | İlk bağlama: bilinmeyen `ref` için önce **aynı tenant'ta**, pasif olmayan, `external_ref`'i boş, e-postası eşleşen çalışan bağlanır (`employee.linked` audit); yoksa yeni satır. Lokasyonlarda otomatik eşleme yok — owner panelde *"External ref"* doldurur | Pilot verisi çift kayda dönüşmesin (API-13 runbook'u) | §9 |
| K-20 | Lokasyon `active` alanı **yalnız etiket**; karar motoruna girmez | Kararı etkilemesi CLAUDE.md §5 + politika + ADR isterdi | §9 |
| K-21 | `employees.updated_at` **tetikleyiciyle**, yalnız rehbere görünen sütunlar değişince (`clock_timestamp()`); rehber `updated_at >= updatedSince − 10 dk` döndürür ve `asOf` verir | Bugün dört `UPDATE employees` yolu var (`invites.sql:250`, `employees.sql:531/612/793`); elle güncellemek birini unuturdu | §9 |
| K-22 | API ve webhook bir tenant için **yalnız operatör açınca**: `tenants.api_enabled_at` **ve** `tenants.api_code` (spec'in `company` alanı, ör. `KF`); açan/kapatan ve kodu yazan tek yazar bir `op_*` (ADR 0021 sınıfı); kapı **her yolda** | `/signup` herkese açık — aksi hâlde herkes sunucumuza istediği adrese istek attırabilirdi; 2. tur: kapı yalnız anahtar yolundaydı | §2 |
| K-23 | API kaynaklı audit: `actor_id = api_keys.id`, `detail.via = "api"` | Aktör bir yönetici değilken izin kaynağı yine adlandırılmalı | §3 |
| K-24 | Çalışan tekilliği `(tenant_id, external_ref) WHERE status <> 'deactivated'`; lokasyon `(tenant_id, external_ref)`. **E-posta tenant içinde, pasif hariç:** `(tenant_id, email) WHERE email IS NOT NULL AND status <> 'deactivated'`, **kısıt adı `employees_tenant_email_key` korunur**; holding genelinde **aranmaz** | Kod çakışmayı kısıt adıyla tanıyor; holding geneli yeni bir tenant-ötesi okuma olurdu (ADR 0027). Yan etkisi ADR 0010 notunda | §9 |
| K-28 | `IsHardened()` = `prod` ∨ `sandbox`; dokuz `IsProd()` çağrısının dokuzu bu yükleme geçer; `EnvDev`'e bağlı üç kapı (`config.go:738`, `devtap.go:71`, `sun/mint.go:78`) sandbox'ta kapalı kalır | `TAPPA_ENV=sandbox` aksi hâlde internete açık, çok taraflı bir kurulumda bu kapıları sessizce kapatırdı (2. tur, iki denetim) | §11 |
| K-31 (olay yarısı) | Holding punch'ının **iki hedef olay satırını da** holding tetikleyicisi yazar; iki sayaç satırı tek ön-kilitle | ADR 0027 §8 | §4, §5 |
| K-32 | `punch_events` olay anında bir **anlık görüntü** taşır (çalışan id/ref/kod/şirket kodu; lokasyon id/ref/ad/şirket kodu; tag uid); API okurken tenant sınırını aşmaz | KM'nin akışı KF lokasyon ref'ini, KF'ninki KM çalışan ref'ini istiyor; okuma anında çapraz join yerine yazma anında değişmez kopya (plan §2 madde 27: punch alanları punch anındaki değerlerdir) | §4 |
| K-37 | **GATE-1:** panelin dört yazma kapısı her dalda `Sec-Fetch-Site: same-site` ve `cross-site`'ı reddeder; diğer her durumda bugünkü davranış aynen | Ucuz sertleştirme; K-3'ün geri dönüşüyle sandbox riski kalksa da `taptime.mt` altındaki her alt alan (ops dahil) için doğru kural | §11 |

*K-27 (olay satırının şirket kimliğini kopyalaması) tek-tenant/iki-şirket modeline aitti; model
değişince (2026-10-10, ADR 0027 K-1) geçersizleşti, yerini K-32'nin anlık görüntüsü aldı.*

### 1. Ortak şema kuralı — `arwd` varsayılanına güvenilmez

Üretimde yeni bir tablo `tappa_app`'e **dört fiille** doğar; taze CI veritabanında `ar` ile — yani
CI fazla yetkiyi göremez. Bu yüzden M11'in **her** yeni tablosunda — ve **kural M11'e özgü
değildir, kalıcıdır**: M11'den sonraki her yeni tablo ve fonksiyon için de geçerlidir (CLAUDE.md §6
ile aynı kapsam; 1. tur düzeltmesi, üçüncü göz):
1. `REVOKE ALL … FROM tappa_app`, ardından **yalnız gereken fiillere ve sütunlara** açık `GRANT`
   (CLAUDE.md §6'nın tablo başına GRANT şartını da bu karşılar);
2. **UPDATE hep sütun listesiyle** (tablo düzeyi UPDATE sütun kısıtını ezer);
3. M11'in her fonksiyonunda `REVOKE ALL ON FUNCTION … FROM PUBLIC` (yeni fonksiyon PUBLIC `EXECUTE`
   ile doğar); tetikleyici fonksiyonlarında `EXECUTE` **yalnız sahipte** ve gövde `TG_RELID`'i assert
   eder (ADR 0027 §2'nin gerekçesi: 4. turun geçici tablo yolu, 6. turun öksüz satırı);
4. **Kabul:** CI migration'lardan **önce** üretimin `arwd` varsayılanını kurar; **genel bir katalog
   testi** M11'in tablo ve fonksiyon listesini dolaşıp her tabloda dört fiili, sütun yetkilerini ve
   her fonksiyonun `EXECUTE` ACL'ini aşağıdaki matrisle karşılaştırır — listeye girmemiş yeni bir
   tablo testi kırmızıya çevirir.

**`tappa_app` matrisi — en az şunlar (normatif; satırların tamamı ilgili görevde bu ilkeyle
yazılır):**

| Tablo | `tappa_app` | Not |
|---|---|---|
| `api_keys` | SELECT; INSERT (sütun listesiyle — API-3); **UPDATE yalnız (`revoked_at`, `last_used_at`)**; DELETE yok | İptal **tek yönlü** — `revoked_at` NULL'a dönemez (00011'in `tappa_forbid_revocation_reset` emsali tetikleyici) ve **her** `revoked_at` yazması `COALESCE(revoked_at, …)` ve/veya `WHERE revoked_at IS NULL` taşır (00011 BOUNDARY 2: korumasız bir toplu iptal tetikleyiciye çarpıp bütün işlemi geri alır, anahtarlar canlı kalırdı) |
| `webhook_endpoints` | SELECT; INSERT; UPDATE (`url`, `signing_key_sealed`, `active`, `rotated_at`) — **`tenant_id` içermez**; DELETE yok | işçi imzalamak için mühürlü anahtarı okur |
| `webhook_deliveries` | SELECT; **UPDATE yalnız kira/sonuç sütunları** (`lease_until`, `attempt`, `next_attempt_at`, `state`, `last_status`, `last_error_class`, `delivered_at`); **INSERT yok**, DELETE yok | teslim satırını yalnız tetikleyiciler yazar |
| `punch_events` | SELECT | INSERT/UPDATE/DELETE yok; append-only |
| `feed_counters` | **yalnız SELECT** | satırı tetikleyiciler açar ve artırır (§5) |
| `site_punches` | SELECT sütun listesiyle, **`sun_valid` hariç** | ADR 0027 §9 |
| `api_idempotency`, `invite_requests` | API-4 / API-6'da bu ilkeyle; UPDATE varsa sütun listesiyle | |
| `tenants` (yeni sütunlar) | `api_enabled_at`, `api_code` üzerinde **UPDATE yok** | yazarı yalnız `op_*` (K-22); API-1 kabulü |
| `holdings`, `holding_members` | **hiçbir fiil** | ADR 0027 §1 |

### 2. Erişim kapısı ve genel yetki kuralı (K-22)

**Kapı — operatör açar.** `tenants.api_enabled_at` (NULL = kapalı) ve `tenants.api_code` (spec'in
`company` alanı; punch'ta `employee.company`/`location.company`) yalnız bir `op_*` ile yazılır (ADR
0021 sınıfı: `tappa_opdefiner` sahipli, `operator_audit_log` + tenant'ın `audit_log`'u, durum
kehaneti yok); `tappa_app` ikisine de UPDATE tutmaz. Kapı **her yolda** uygulanır: anahtar çözümü
(kapalı → 403), uç nokta oluşturma, test ping, yeniden gönderim, tetikleyicinin teslim satırı yazması
ve işçinin **gönderim anı**. Kapalı tenant'ın olayları yine yazılır (imleçli akış süreklidir); teslim
satırı yazılmaz.

**Yetki — normatif:**
- **Yazma** (`PUT`/`DELETE`): yalnız anahtarın tenant'ının çalışan ve lokasyonları; arama
  `(tenant_id, external_ref)` ile, `WithTenant(anahtarın tenant'ı)` + açık `tenant_id` filtresi.
- **`GET /v1/punches?scope=location`**: hedef tenant = anahtarın tenant'ı ∧ olay satırının rolleri
  `location` içerir. **`scope=employer`**: hedef tenant = anahtarın tenant'ı ∧ roller `employer`
  içerir. Olay satırları zaten hedef tenant'ta durur (§4); okuma tenant sınırını **aşmaz**.
- **Rehber** (`GET /v1/directory`) iki kaynağın birleşimidir (1. tur düzeltmesi, üçüncü göz):
  anahtarın tenant'ının **kendi** çalışanları **her zaman** sıradan, RLS'li, açık `tenant_id`
  filtreli bir sorgudan; **eşlerin** çalışanları `holding_directory`'den (ADR 0027 §2) — o fonksiyon
  **yalnız eşleri** döner, çağıranın kendi tenant'ını **asla** (`holding_peer(a, a, …) = false`).
  İkisi de e-postasız, sayfalı (tek imleçte birleşik sıra API-8'de).
- **Holding dışı tenant'ın anahtarı yalnız kendi tenant'ını görür:** rehberde yalnız kendi
  çalışanları (RLS'li yol); `holding_directory` onun bağlamında **0 satır** döner (K-33'ün *"C
  bağlamında 0 satır"* kuralı aynen); akışta yalnız kendi hedef satırları.

### 3. API anahtarı

- **Tablo:** `api_keys(tenant_id, id, name, token_hash, prefix_hint, created_by, created_at,
  last_used_at, revoked_at)` — CLAUDE.md §6'nın RLS beşlisi; `token_hash` **küresel UNIQUE**.
- **Biçim (K-10):** `tt_live_` + 43 karakter base64url (32 bayt rastgele); sandbox'ta `tt_test_`.
  Saklanan değer, kendi env anahtarıyla (`TAPPA_API_KEY_HMAC_KEY`) HMAC-SHA256'dır — veritabanı
  dökümü tek başına anahtar denemesine yetmez. Ham anahtar yalnız oluşturma yanıtında, **bir kez**.
- **Kimlik zinciri** (sıra normatif): `Authorization: Bearer` → **biçim kontrolü** (ortamın öneki +
  43 base64url; değilse **DB'ye gitmeden 401**) → **IP başına kova** (çözümleyiciden önce; geçersiz
  anahtar seli veritabanına ulaşmaz) → **yedinci çözümleyici** → **anahtar başına kova**.
- **Yedinci çözümleyici `resolve_api_key_by_hash(p_token_hash)`** — ADR 0002 md.7'nin beş kısıtına
  birebir: `tappa_resolver` sahipli `SECURITY DEFINER`; girdi yalnız hash; dönüş `token_hash`
  UNIQUE'iyle **en çok bir satır**; sabit `search_path = pg_catalog, pg_temp`; nitelikli adlar;
  dinamik SQL yok; `tappa_app`'e yalnız `EXECUTE`. Mevcut altı çözümleyicinin **"taşı,
  uygulama"** ilkesine uyar (`internal/db/resolve.go`): dönüş `(tenant_id, key_id, revoked_at,
  api_enabled_at)`, karar uygulamada ve **aynı istekte** — iptal → **401**, tenant'ın API erişimi
  kapalı → **403**. **Önbellek yok** (iptal ve kapatma bir sonraki istekte geçerli).
  `tappa_resolver`'a yeni yetki: `api_keys` üzerinde çözümlemenin sütunları ve **yalnız** `SELECT
  (id, api_enabled_at) ON tenants` (birincil anahtar join'i — md.7 (ii) bozulmaz; ADR 0002 notu).
  Sonraki her sorgu `WithTenant` + açık `tenant_id` filtresiyle.
- **Panel (owner):** oluştur (bir kez göster) · listele (ad, önek ipucu, son kullanım) · iptal.
  Audit `api_key.created`, `api_key.revoked`; `last_used_at` dakikada en çok bir kez yazılır. API
  kaynaklı her audit satırı `actor_id = api_keys.id`, `detail.via = "api"` (K-23).
- **Log ve tarama — asla loglanmaz:** anahtarın kendisi ve hash'i, `Authorization` başlığı, uç
  noktanın imza anahtarı, webhook URL'si ve `*url.Error` metni (Go o metinde yalnız URL'nin
  parolasını siler, sorguyu korur) — hata mesajı ve audit `detail`'i dahil (CLAUDE.md §7). Tarayıcı
  eklemeleri (API-3) **önce bugünkü ağaçta sayılır** (3. tur: yalın `kek`/`signature`/`authorization`
  kökleri bugün 35 satırı kırmızıya çevirirdi ve R7'nin *"kelimeyi değil değeri hedefle"* ilkesine
  aykırıdır):
  - **R7:** yalnız ağacı kırmızıya çevirmeyen kökler (`api_?key`, `bearer`, `signing_?key`) ve log
    çağrısının içinde `Header.Get("Authorization")` / `Header["Authorization"]` arayan **dar** bir
    kalıp;
  - **R7b:** yeni kişisel ad alanları (`first_name`, `last_name` ve izdüşümün `employee_name`'i);
  - **R7d:** iki **önekli değer** kalıbı — API anahtarı öneki ve uç nokta imza anahtarının öneki (§6);
  - **R4:** şema nitelikli `UPDATE public.tags SET last_ctr` biçimi (bugün görmüyor —
    `scripts/redline-check.sh:345`; holding ilerletmesi bu biçimdedir — ADR 0027 §12);
  - her ekleme **kasıtlı ihlalde kırmızı** testiyle.
- **Env anahtarları** `TAPPA_API_KEY_HMAC_KEY` ve `TAPPA_WEBHOOK_KEK` `namedKeys()`'e girer ve
  **bütün** anahtarlarla çift çift karşılaştırılır (bugün ayrım kontrolü yalnız operatör çiftlerine
  bakıyor — `config.go:688-692`; genelleşir). Genelleştirme ölçülmemiş bir üretim yapılandırmasının
  açılışını reddettirebilir → API-13 runbook'u deploy'dan önce prod'daki anahtar çiftlerinin
  eşitliğini **değerleri basmadan** ölçer.

### 4. Punch olayları — yapısal yayın (K-4, K-5, K-6, K-31, K-32)

Dışarıdaki punch, `transactions` satırının **dışarıya bakan izdüşümüdür**; Go katmanı değil,
**veritabanı tetikleyicisi** yazar — binary sürümünden bağımsız (eski binary ve geri dönüş de olay
doğurur), saf `tap.Decide`, `internal/policy` ve sayaç ilerletme yolu **dokunulmaz**.

**Tablolar (API-2):**

```
punch_events(tenant_id, seq, punch_id, punch_tenant_id, roles, version, status, punch_time,
             recorded_at, channel, employee_id, employee_ref, employee_code, employee_company,
             location_id, location_ref, location_name, location_company, tag_uid, created_at)
feed_counters(tenant_id, last_seq)
webhook_endpoints(…), webhook_deliveries(…)   -- §6
```

- `punch_events.tenant_id` = **hedef** tenant (olayı okuyacak olan); `roles ⊆ {employer, location}`;
  `UNIQUE(tenant_id, seq)`; FK `(punch_id, punch_tenant_id) → transactions(id, tenant_id)`
  (holding punch'ında saha satırının `punch_tenant_id`'si işverendir); **anlık görüntü** sütunları
  (K-32) olay anında yazılır ve bir daha değişmez; append-only (`UPDATE`/`DELETE`/`TRUNCATE`
  tetikleyicileri, `transactions` gibi); R3 tarayıcısının kapsamına girer.
- `version = 1`, `status = 'valid'` (K-5). `punch_time` = `occurred_at` (telefonun beyan ettiği an);
  `recorded_at` = sunucunun yazdığı an.
- **Akışa giriş şartı (K-4):** `verdict='ok'` ∧ `channel ∈ {nfc, qr}` ∧ ¬`practice`; ya da aynı
  kanal/practice şartını sağlayan bir `flag` satırının **onayı** (`transaction_reviews.outcome =
  'approved'`) — onay anında, onayın kendi olayı olarak. Manuel (K-6), practice, `reject`, `ignored`
  ve reddedilmiş flag **asla**.

**Hedef satırlar:**
- **Tek-tenant punch** → bir satır: hedef = tenant, `roles = {employer, location}`.
- **Holding punch'ı** (`site_tenant_id` dolu — ADR 0027) → iki satır: işveren `{employer}`, plaketin
  tenant'ı `{location}`; ikisini de **holding tetikleyicisi** yazar (K-31). İşverenin satırında saha
  lokasyonunun id/ref/ad/şirket kodu, sahanın satırında işverenin çalışan id/ref/kod/şirket kodu
  vardır (plan §2 madde 27). Eşlik bitmişse yalnız işverenin satırı, karşı taraf anlık görüntüsü
  **NULL** olarak yazılır (ADR 0027 §8).

**İki tetikleyici ve ad sırası (normatif).** Aynı olaya bağlı iki `AFTER INSERT` tetikleyicisi
(`transactions` ve `transaction_reviews` üzerinde) PostgreSQL'de **ad sırasıyla** çalışır; sıra
katalog testinde pinlenir:
1. **Holding tetikleyicisi** (`tappa_holdingdefiner`, BYPASSRLS — **ad sırasında ilk**): yalnız
   `site_*` taşıyan satırlar ve onlara bağlı onaylar. Sıra: eşlik kararı → (olay yazılacaksa) iki
   sayaç satırını *"var et"* + kanonik tek ifadeli kilit + `= 2` assert'i → olay ve teslim satırları
   → (eşlik sürüyorsa, K-4'ten bağımsız) `site_punches`. Ayrıntı ADR 0027 §8.
2. **Kendi-tenant tetikleyicisi** (`tappa_feedwriter`, NOBYPASSRLS — ad sırasında ikinci): `site_*`
   taşıyan satırlara ve onlara bağlı onaylara **hiç dokunmaz**; yalnız `site_tenant_id IS NULL`
   satırların olay ve teslim satırlarını yazar.

**`tappa_feedwriter` — `tappa_app` olay yazamaz.** Kendi-tenant tetikleyici fonksiyonları
`SECURITY DEFINER`, sahibi `NOLOGIN NOBYPASSRLS` ve **üyesiz** `tappa_feedwriter` (3. turda ölçüldü:
definer içinde `current_setting('app.tenant_id')` çağıranın değerini verir, RLS `WITH CHECK`
uygulanır, FORCE RLS altında sütun SELECT'i yeterlidir, `tappa_app`'in doğrudan INSERT'i 42501). Rol
tenant sınırını **aşmaz** (politikalar GUC'a bağlı); yalnız yazma hakkını tetikleyiciye kilitler.
Yetkisi — **bu modelde yeniden ölçülecek liste** (5. sürümde 25/26 kalem ölçülmüştü; şirket sütunları
çıktı, `site_tenant_id` ve anlık görüntünün sütunları girdi → API-2 *"her biri tek başına zorunlu"*
kanıtıyla pinler):
- `punch_events` INSERT; `webhook_deliveries` INSERT;
- `feed_counters` SELECT (`tenant_id`, `last_seq`), INSERT, UPDATE (`last_seq`) — `ON CONFLICT DO
  UPDATE … RETURNING` SELECT ister (3. tur: SELECT'siz ilk dokunuş 42501);
- `USAGE ON SCHEMA public` (bu kümede PUBLIC'in `USAGE`'ı yok — 4. tur);
- okuma sütunları (birleştirme anahtarları ve açık tenant filtresi sütunları dahil — 4. tur: eksik
  bir anahtar sütunuyla `WHERE id = …` 42501): `transactions` (`id`, `tenant_id`, `channel`,
  `practice`, `verdict`, `site_tenant_id`, `location_id`, `employee_id`, `tag_uid`, `occurred_at`,
  `created_at`), `employees` (`id`, `tenant_id`, `external_ref`, `employee_code`), `locations`
  (`id`, `tenant_id`, `external_ref`, `name`), `tenants` (`id`, `api_code`, `api_enabled_at`),
  `webhook_endpoints` (`id`, `tenant_id`, `active`) — **adres ve imza anahtarı asla**.

**Tetikleyicilerin değişmezleri** (ikisi için; API-2'de testli): `SET search_path = pg_catalog,
pg_temp` ve bütün adlar nitelikli; her sorguda açık `tenant_id` (kuşak + kemer); kendi-tenant
kısmında `NEW.tenant_id` = GUC **assert**'i; `TG_RELID` assert'i; **hata yutan `EXCEPTION` bloğu
yok** (savepoint geri alımı sayaç kilidini bırakır — ölçüldü — ve olay sessizce düşerdi; eşlik
bitince atlamak açık bir `IF`'tir); uç nokta **adresini okumaz** (bozuk bir uç nokta tap yolunu
kıramaz); ağ ya da uzun iş yok; öznitelik olarak kısa bir `lock_timeout` (§5).

**Teslim satırı:** olay satırını yazan tetikleyici, **hedef tenant'ın** API erişimi açıksa (K-22) ve
**aktif** bir uç noktası varsa, o uç noktaya bir `webhook_deliveries` satırı yazar. Ayrı yayılım
taraması yok.

**Rollerin yaşam döngüsü ve katalog** (iki definer — `tappa_feedwriter` ve ADR 0027'nin
`tappa_holdingdefiner`'ı): roller yalnız `scripts/db-init/01-roles.sql`'in **idempotent** bloğunda
(OP-5 kalıbı) ve canlı küme için `deploy/README.md` runbook'unda doğar; `pg_dump` rol taşımadığı
için (`scripts/pg-restore-verify.sh:379`) geri yükleme provası **sıfırdan kurulmuş** bir pod'a.
Migration'ın ön koşul bloğu rol yoksa ya da rolün **iki yönden** herhangi bir üyeliği varsa düşer
(`db/migrations/00029_read_tenants_from_the_operator.sql:99-120` emsali). Migration sonrasındaki
hatalı bir `GRANT <definer> TO tappa_app` için çalışan kapı: `internal/db/pool.go`'nun rol reddi,
**sertleştirilmiş** ortamda (K-28) `tappa_app` **herhangi bir rolün üyesiyse** açılışı reddeder
(bugün süper kullanıcılık, BYPASSRLS, tablo sahipliği, bunlara üyelik ve rol değiştirmiş oturumu
denetliyor; genel üyelik eklenir — 5. tur ölçtü: taze init'te ve dev'de `tappa_app`'in üyeliği 0,
kural bugünkü açılışı kırmaz; API-13 runbook'u prod'da `pg_auth_members`'ı ölçer). **Katalog**
(`internal/db/operatorschema_test.go` kalıbı): iki rol NOLOGIN, beklenen BYPASSRLS değeri
(`tappa_feedwriter` `f`, `tappa_holdingdefiner` `t`), üyesi 0; `prorettype = trigger` olan her
fonksiyonda `EXECUTE` yalnız sahipte; **tetikleyici ad sırası**; `proconfig` sabit (`search_path`,
`lock_timeout`); ADR 0027'nin K-34 sütun pinleri; ADR 0021'in *"`prosecdef` sahibi yalnız
{`tappa_resolver`, `tappa_opdefiner`}"* testi iki rolle genişler (ADR 0021 notu).

**Geri doldurma** (tek migration işlemi): önce `transactions` **ve** `transaction_reviews`
kilitlenir, tetikleyiciler kurulur, **sonra** geçmiş uygun tek-tenant satırlar için olay yazılır
(sıra `occurred_at`; sayaçlar buna göre ilerler; bugün holding satırı yok). Geri doldurulan olaylar
için teslim satırı yazılmaz. Kilit süresi prod boyutunda bir kopyada ölçülür; **eşik 2 sn** — aşılırsa
geri doldurma tenant başına küçük işlemlere bölünür.

**Bedel (adlandırılmış):** tetikleyicinin her hatası `ok` dokunuşun kaydını **geri alır** (sayaç
ilerlemiş, kayıt yok — bugünkü her INSERT hatasıyla aynı sınıf); başarısız kayıt kimlikleriyle
loglanır (sır yok). Yeni tenant'lar arası bağımlılık ADR 0027 §8'de.

**Eşlik eden listeler:** `cmd/tappa/scriptguards_test.go:447-510` (`APPEND_ONLY` — `punch_events`,
`site_punches`), `scripts/pg-restore-verify.sh` (`trunc_tables` ve sabit *"seven"* başarı cümlesi).

### 5. Akış imleci — neden kaybolmaz (K-13, K-31)

Her hedef satırdan önce hedef tenant'ın sayacı

```
INSERT INTO feed_counters … ON CONFLICT (tenant_id) DO UPDATE SET last_seq = last_seq + 1 RETURNING last_seq
```

ile artar. **İspat:** satır kilidi commit'e kadar tutulur; aynı hedefte iki olay yazan işlem sayacı
**sırayla** artırır, `seq`'i sırayla alır ve **o sırayla commit eder**. Bir okuyucu `seq = S`'ye
kadar commit edilmiş olayları gördüğü anda, o tenant'ta commit etmemiş olay **en çok bir** tanedir
(satırı kilitleyen) ve `seq`'i `S`'den büyüktür; sonrakiler daha da büyük. Dolayısıyla `WHERE
tenant_id = $t AND seq > $cursor ORDER BY seq` **hiçbir satırı atlamaz**. Abort eden işlem sayacı da
geri alır (boşluk bile kalmaz). Dizi yok → önbellek sorunu yok; sayaç tenant'a özgü → tenant'lar
arası hacim sızmaz. İmleç `seq`'i opak taşır; tenant ve kapsam filtresi her sorguda ayrıca.

**Ölçümler:** 2. tur — küresel dizi (`CACHE 20`) kilit altında 1234 olayın 1016'sını kaçırdı;
3. tur — sayaç satırıyla 3 koşuda (1255–2416 olay, %10 abort, %10 savepoint geri alımı) **0 kaçan**;
kilitsiz dizi ve ayrı işlemde sayaç mutasyonları 843 / 408 kaçırdı; ilk-olay yarışı 1700 olayda 0.

**İki hedef (holding punch'ı):** K-31'in tek ön-kilidi (ADR 0027 §8) — 7. tur: 14 288 işlemde
(8 106 holding) **0 kilitlenme, 0 kaçan**; ön-kilitsiz mutant 20 turda 9 kilitlenme; sayaç satırı
eksikken *"var et"* adımıyla 8 640 işlemde 0 kilitlenme (mutant 324). API-2 stresi ters yönlü
holding dokunuşlarıyla yeniden ölçer.

**Savepoint:** sayaç güncellemesi ile olay INSERT'i aynı alt işlemdedir; geri alınırsa ikisi birlikte
gider, kilit bırakılır — tutarlı (`EXCEPTION` yasağı bunu korur).

**Kilit değişmezi:** sayaç satırları kilitlendikten sonra işlemde **çakışabilecek** hiçbir kilit
alınmaz ve ağ/uzun iş yapılmaz (onay yolunda ardından gelen `audit_log` INSERT'i ve teslim satırının
FK'sinin aldığı KEY SHARE çakışmaz — 2. tur kodda doğruladı).

**Takılı yazıcı** (ölçüldü: tenant'ın 8 yazıcısının 8'i bekledi; holding'de KF'deki takılı işlem KM'yi
de bekletir — ADR 0027 §8). API-2'nin ön koşulu:
- `tappa_app` bağlantıları için `idle_in_transaction_session_timeout` ve TCP keepalive — **havuzun
  `RuntimeParams`'ıyla** verilir ve açılışta `SHOW` ile doğrulanır (`ALTER ROLE … SET` `pg_dump`'la
  taşınmaz, geri yüklemede sessizce kaybolurdu);
- tetikleyici fonksiyonlarının **özniteliği** olarak kısa bir `lock_timeout` (`proconfig` — fonksiyon
  çıkışında eski değere döner; gövdede `set_config(…, true)` işlemin geri kalanına sızıyordu, 3. tur);
  aşılırsa dokunuş *"tekrar dene"* alır (sayaç ilerlemiş, kayıt yok; 3. turda ~505 ms'de 55P03).
- **Başlangıç değerleri** (yapıcının seçimi; API-2 ölçerek pinler, değişiklik bu ADR'ye nottur):
  tetikleyici `lock_timeout = 2s` (normal kilit tutma süresinin çok üstünde, 30 sn istek zaman
  aşımının çok altında); `idle_in_transaction_session_timeout = 30s` (istek zaman aşımına eşit — hiçbir
  meşru yol bir isteğin ömründen uzun boşta işlem tutmaz); TCP keepalive `idle 30 s · interval
  10 s · count 3`.

**Sayaç satırı her tenant için her zaman vardır:** migration mevcut tenant'lara açar; yeni tenant'ınkini
`tappa_feedwriter` sahipli bir **`AFTER INSERT ON tenants`** tetikleyicisi açar (signup kodu değil:
`tappa_app`'in `feed_counters`'a INSERT'i yok; tetikleyici M11 öncesi bir binary'ye geri dönüşte de
çalışır). Yine de holding ön-kilidi satırları önce *"var eder"* ve kilitlediği sayıyı assert eder
(K-31). **Migration sırası:** tetikleyiciler geri doldurmadan **önce** kurulur.

**Geri yükleme:** sayaçlar `pg_dump`'la yedek anındaki değerle gelir; yedekten sonra tüketilmiş olay
varsa tüketicinin imleci onların ötesindedir. Bu yüzden **uygulama yazmaya başlamadan önce** geri
yükleme runbook'u her tenant'ın sayacını büyük bir aralık ileri atar; doğrulayıcı
`count(tenants) = count(feed_counters)` ve her tenant için `last_seq ≥ max(punch_events.seq) +
aralık` şartını geri yüklenmiş verinin kendisinden denetler, sağlanmıyorsa reddeder (bugünkü
ENABLE/FORCE sayım kapısının emsali). Bugün gece geri yükleme provası yok
(`deploy/k8s/50-backup.yaml` yalnız yedek alır); bu adım T45 tipi elle provada sınanır.

**Geçmiş sorgusu** (`from`/`to`): `punch_events` üzerinden, ≤ 31 gün, sayfalı.

### 6. Webhook (K-11, K-12, K-19)

**Uç nokta:** `webhook_endpoints(tenant_id, id, url, signing_key_sealed, active, created_at,
rotated_at)` — RLS beşlisi, `UNIQUE(tenant_id)` (tenant başına tek uç nokta). İmza anahtarı
`sun.Seal(TAPPA_WEBHOOK_KEK, AAD = endpoint id)` ile mühürlü (kendi KEK'i — CLAUDE.md §4.7'nin
sınıfı); **önekli** üretilir (önek API-9'da seçilir; R7d'nin ikinci değer kalıbı odur); oluşturma ve
döndürmede **bir kez** gösterilir, *"göster"* yok (K-12). Rehberdeki bilinen-cevap vektörünün
anahtarı **açıkça sahtedir**.

**URL kuralı:** yalnız `https`, port **443**, **IP literali yok**, **userinfo yok**, **sorgu dizgesi
yok**, **parça yok** (spec'in adresleri bu biçimde). URL ve `*url.Error` metni **asla** log'a ya da
audit'e yazılmaz; log ve audit yalnız uç nokta kimliği + hata sınıfı taşır.

**Teslim:** `webhook_deliveries(tenant_id, id, endpoint_id, punch_event_seq, attempt,
next_attempt_at, lease_until, state, last_status, last_error_class, created_at, delivered_at)` —
`UNIQUE(endpoint_id, punch_event_seq)`; `state ∈ {pending, delivered, dead}`; `last_error_class`
**kapalı küme** (`timeout` · `tls` · `refused` · `http_4xx` · `http_5xx` · `blocked_address`);
uzak sunucunun yanıt metni **saklanmaz, loglanmaz**.

**Yönlendirme:** her hedef olay satırı **kendi tenant'ının** uç noktasına bir teslim → KF@KF 1 ·
KM@KM 1 · KM@KF 2 (KM `employer`, KF `location`) · KF@KM 2. Yeni uç nokta yalnız oluşturulduktan
sonraki olayları alır. Sıra garantisi yok; tekilleştirme `id` + `version` ile (plan §2 madde 16).

**Yük:** spec §5'in alanları + `channel` + anlık görüntü (K-32); **IP, GPS, mesafe, not, policy
bağlamı asla**. Test ping sentetiktir (`event: "ping"`). Ping **ve** panelden *"yeniden gönder"* aynı
kovayı paylaşır: tenant başına **saatte 10**.

**İmza (K-11):** `sha256=` öneki + gövdenin, uç noktanın imza anahtarıyla hex HMAC-SHA256'sı; ek
zaman damgası başlığı yok. Tappa'nın kendi bilinen-cevap vektörü rehbere konur (API-12).

**İşçi — kiralama modeli:** (1) vadesi gelen teslimi kısa bir işlemde `FOR UPDATE SKIP LOCKED` ile
seç, `lease_until` yaz, commit; (2) HTTP isteğini işlem **dışında** gönder; (3) sonucu ayrı kısa
işlemde yaz. Gönderim anında **ve** *"yeniden gönder"*de tenant'ın API erişimi (K-22) **ve** uç
noktanın `active`'i yeniden denetlenir — devre dışı bırakılan uç noktaya kuyruktaki teslimler gitmez
(K-26'nın savunma yazması gerçekten durdurur). Geri çekilme **1 dk · 5 dk · 30 dk · 2 sa · 6 sa →
`dead`**. Panelde teslim günlüğü + *"yeniden gönder"*. İki pod aynı teslimi bir kez gönderir (kira).

**İşçi kurulumu — K-19 (tenant'lar arası tarama YOK):** süreç içi *"işi olan tenant'lar"* kümesi.
Bir tenant kümeye, o tenant'ta bir dokunuş ya da onay commit edildiğinde (**holding punch'ında iki
tenant da** girer), kimliği doğrulanmış her API isteğinde ve panelden *"yeniden gönder"*de girer;
işçi **`WithTenant` içinde** yalnız o tenant'ın vadesi gelmiş teslimlerini ve davet isteklerini
okur. Pod yeniden başlarsa küme boştur (plan §2 madde 20). Hiçbir yol bütün tenant'ları dolaşmaz.

**Kapanış bütçesi:** işçi `srv.Shutdown` ile **eşzamanlı** boşalır, 20 sn penceresinin içinde (bugün
pay: 20 + 5 sn, `terminationGracePeriodSeconds: 30`); uçuştaki istek iptal edilir, kira dolunca
yeniden denenir. API-10 pinler.

### 7. SSRF — tehdit modeli ve izin listesi

**Neden bir tehdit modeli.** Egress açıktır (plan §6, §8: egress NetworkPolicy kapsam dışı) —
**bu kapı tek savunmadır.**

**Saldırgan:** API erişimi açılmış (K-22) herhangi bir tenant'ın **owner'ı** — ya da owner'ın panel
oturumunu ele geçiren biri. Adresi o girer (K-12; webhook yönetimi API'den yapılamaz — plan §8). Tenant
açmak herkese açıktır (`/signup`); K-22 kapısı bu yüzden önkoşuldur.

**Hedefler (korunan):** kümenin iç ağı (Postgres, Kubernetes API sunucusu — `kubernetes.default`
443'te dinler —, servis ve pod adresleri), bulut meta veri uç noktaları (IPv4 link-local, IPv6 ULA),
düğümün loopback'i, **düğümün kendi genel adresleri ve ingress'i** (443'ten Tappa'nın kendisine
dönmek: kendi API'sine imzalı istek, oran sınırını atlatma), özel ve ayrılmış her adres bloğu.

**Saldırı yolları ve kapatan kural:**

| Yol | Kural |
|---|---|
| URL'ye özel bir IP yazmak | URL'de IP literali yok; ve kontrol zaten dial anında |
| Genel bir ad, özel bir adrese çözülür | Kontrol **`net.Dialer.ControlContext`** içinde, **gerçekten bağlanılan** adrese — çözümlemeye değil |
| **DNS rebinding** (ilk çözüm genel, ikincisi özel) | Her dial'da yeniden kontrol (Happy Eyeballs denemelerinin her biri dahil); API-10 iki aşamalı DNS testiyle sınar |
| IPv4-eşlemeli IPv6 (`::ffff:a.b.c.d`) | Adres önce **`Unmap()`** edilir, sonra IPv4 kuralları |
| Gömülü IPv4 taşıyan IPv6 biçimleri — 6to4 (`2002::/16`), Teredo (`2001::/32`), NAT64 (`64:ff9b::/96`), IPv4-uyumlu | IPv6 **yalnız `2000::/3`** içinden kabul; `2001::/23`'ün tamamı ve `2002::/16` düşülür; NAT64 ve IPv4-uyumlu biçimler `2000::/3` dışında kalır. Go'nun `IsGlobalUnicast`'ı bunlara `true` der — **güvenilmez** |
| HTTP yönlendirmesiyle iç adrese sekme | Yönlendirme **izlenmez** |
| Ortam değişkeniyle proxy (`HTTPS_PROXY`) | `Transport.Proxy: nil` — proxy olsa da doğrudan bağlanılır (API-10 testi) |
| Uzun / büyük yanıtla kaynak tüketme | toplam **10 sn**; `MaxResponseHeaderBytes` sınırlı; gövde **okunmaz** (en çok 1 KiB atılır) |
| Yanıtı okuyarak iç veriyi sızdırma | gövde okunmaz, yanıt metni saklanmaz/loglanmaz; owner'a yalnız kapalı küme hata sınıfı ve durum kodu |
| Hata metniyle URL/sorgu sızdırma | URL'de sorgu ve userinfo yok; `*url.Error` metni log/audit'e asla |
| TLS'i düşürme | yalnız `https`; doğrulama Go varsayılanı (atlama yok) |
| Teslim seli / yansıtıcı olarak kullanma | tenant başına tek uç nokta; ping + yeniden gönderim saatte 10; teslimler yalnız gerçek punch'lardan; geri çekilme; K-22 kapısı her yolda |

**İzin listesi — normatif (dial anında, `Unmap()`'ten sonra):**
- **İki aile için:** adres **IANA özel amaçlı adres kayıtlarının** (*IPv4 Special-Purpose Address
  Registry*, *IPv6 Special-Purpose Address Registry*) **hiçbir girdisinde** değildir — liste kayıttan
  **birebir** alınır, elle seçilmez (ör. IPv6 belgeleme bloğu `2001:db8::/32`, `2001::/23`'ün dışında
  olduğu hâlde bu kuralla düşer).
- **IPv4** ayrıca küresel unicast'tir; en az `0.0.0.0/8`, `10.0.0.0/8`, `100.64.0.0/10`,
  `127.0.0.0/8`, `169.254.0.0/16`, `172.16.0.0/12`, `192.0.0.0/24`, `192.0.2.0/24`, `192.88.99.0/24`,
  `192.168.0.0/16`, `198.18.0.0/15`, `198.51.100.0/24`, `203.0.113.0/24`, `224.0.0.0/4`,
  `240.0.0.0/4` reddedilir.
- **IPv6** ayrıca **yalnız `2000::/3`** içindendir ve **`2001::/23`'ün tamamı** (Teredo, PCP/TURN/SRP
  anycast `2001:1::1`–`::3`, benchmarking `2001:2::/48`, ORCHIDv2, AMT — 3. tur `netip` ile ölçtü),
  **`3fff::/20`** (RFC 9637) ve **`2002::/16`** düşülür.
- **Düğümün kendi genel adresleri ve ingress adresi** değildir — bir config listesi; liste **boşsa
  prod ve sandbox başlamaz**.
- Port yalnız 443.

**Kapatılmayanlar — sayılı:** (a) **genel internete kör istek** tasarım gereğidir (webhook budur):
owner Tappa'yı herhangi bir genel HTTPS uç noktasına, gerçek punch hızında ve saatte 10 ping/yeniden
gönderimle, imzalı JSON POST'larıyla yönlendirebilir; (b) owner, kendi uç noktasının durum sınıfını
görerek genel bir adresin 443'te erişilebilirliğini öğrenebilir — yalnız genel internet için; (c)
egress NetworkPolicy yok (plan §8) — bu kapının bir kusuru doğrudan bir SSRF'tir; bu yüzden
kural listesi testle değil **kayıttan** türetilir ve API-10'un ret listesi her kaydı dolaşır.

### 8. Ortak sözleşme (K-8)

- **Hata gövdesi:** JSON `{code, message, details}`; 400 / 401 / 403 / 404 / 409 / 422 / 429 / 503;
  429'da `Retry-After`. `limit` ≤ 1000 (varsayılan 500; 1001 → 400). İstek gövdesi ≤ **64 KiB**.
- **Idempotency-Key** (yazma isteklerinde opsiyonel): `api_idempotency(tenant_id, key_id, idem_key,
  request_hash, status, result_ref, created_at)`, `UNIQUE(tenant_id, key_id, idem_key)`. Satır **iş
  işlemiyle aynı işlemde** yazılır: eşzamanlı ikinci istek UNIQUE indekste ilk commit edene kadar
  bekler, sonra kayıtlı sonucu döner; süreç ortada ölürse işlem geri alınır, takılı bir *"işleniyor"*
  kaydı kalmaz. **Yanıt gövdesi saklanmaz** (yalnız durum + kaydın kimliği). Aynı anahtar + farklı
  gövde → **422**. 24 sa sonra istek yolunda, o tenant için **tembel** silinir (tenant'lar arası
  temizlik yok — K-19).
- `GET /v1/employees` yanıtı **e-posta içermez**; `activationEmail` durumu ve zamanı içerir.
- Dış kimlikler `emp_` / `loc_` / `pch_` / `dlv_` + UUID; `tag_` + uid (QR satırlarında da `tagId`
  doludur).
- **OpenAPI 3.1 elle yazılır**, binary'ye embed edilir, `GET /v1/openapi.yaml` (Node yok, üretici
  yok — CLAUDE.md §1).
- **Sözleşme farkları** — entegratöre bildirilecek 31 madde plan §2'dedir ve API-12'nin rehberine
  **birebir** girer (madde 31 — `scope=location` ile süre hesaplama uyarısı — zorunlu). Bu ADR'de
  kararı olan maddeler: 1 (K-2), 2 (K-4), 3 (K-5), 6 (debounce iki şirketin plaketleri arasında —
  ADR 0027), 7 (K-7), 8 (K-20), 11–14 (K-15, K-17, K-24), 17 (K-6), 19 (K-21), 20 (K-19), 22 (K-25),
  23 (K-22), 24 (K-14), 27 (K-32), 29 (rehber sayfalı).

### 9. Çalışan ve lokasyon uçları, davet kuyruğu (K-7, K-14 … K-18, K-20, K-21, K-24)

**Şema (API-1):** `employees`'e `external_ref`, `employee_code`, `first_name`/`last_name`,
`updated_at` (K-21 tetikleyicisi); `locations`'a `external_ref`, `active`; `invite_requests`; `tenants`'a
`api_enabled_at`, `api_code` + yazarı `op_*`. `employees.location_id` API'den yönetilen satırlarda NULL
olabilir (K-14) — **API-1 önce `location_id`'nin bütün okuyucularını sayar** (bugün ör.
`db/queries/transactions.sql:354` `e.location_id` okuyor); kabul edilemezse geri dönüş opsiyonel
`homeLocationRef`'tir.

**Tekillik (K-24):** çalışan `(tenant_id, external_ref) WHERE status <> 'deactivated'`; lokasyon
`(tenant_id, external_ref)`; e-posta `(tenant_id, email) WHERE email IS NOT NULL AND status <>
'deactivated'` — **kısıt adı `employees_tenant_email_key` korunur** (`staff.go:706-717` çakışmayı
adıyla tanıyor). Pasif bir satırla aynı e-postada yeni aktif satır açılır; iki aktif açılamaz. Holding
genelinde aranmaz (ADR 0027 §13 madde 10).

**Uçlar:**
- `PUT /v1/locations/{ref}`: farklı `timezone` → 422 (K-7); `active` yalnız etiket (K-20); tekrar
  `PUT` yeni satır açmaz; API ile açılan lokasyon panelden GPS/IP tanımlanana kadar dokunuşları onaya
  düşer (plan §2 madde 10). Lokasyonlarda otomatik eşleme yok (K-18).
- `PUT /v1/employees/{ref}`: bilinmeyen `ref` için önce K-18 bağlaması, yoksa yeni satır + **otomatik
  davet** (K-16, kuyruğa). E-posta ASCII olmalı ve tenant'ın bir yönetici adresi olamaz (422); tenant
  içinde aktif başka çalışanda kullanılıyorsa **409 `email_unavailable`** (yeni satır açılmaz, davet
  gitmez). Pasif çalışana `active:true` → **yeni satır** (yeni `employee.id`, aynı `externalRef`;
  eski geçmiş yerinde — ADR 0010). E-posta değişince bekleyen davetler **aynı işlemde** ölür; aktive
  değilse yeni davet kuyruğa, aktive ise gitmez (K-15).
- `GET /v1/employees[/{ref}]` — e-postasız.
- `DELETE /v1/employees/{ref}` ve `PUT {active:false}` — pasifleştirme, **bütçeli** (§12).
- `POST /v1/employees/{ref}/resend-activation`: kuyrukta canlı istek ya da son 1 saatte gönderilmiş
  canlı davet varken **etkisiz** (202); aktive çalışana **409 `already_activated`** (cihaz değişimi
  yalnız panelden).
- `GET /v1/directory?updatedSince=…&cursor=…` — kendi çalışanları RLS'li sorgudan + eşlerinki
  `holding_directory`'den (ADR 0027; §2), 10 dk örtüşme penceresi, `asOf`, sayfalı (≤ 1000),
  aktif/pasif durumu (K-1h), e-postasız.

**Davet kuyruğu (K-17):** `invite_requests(tenant_id, employee_id, requested_at, lease_until,
attempts)` — `employees`'e kira sütunu eklenmez (rehberin `updated_at`'ını kirletmesin). İşçi
(`internal/invite`, K-19 kümesiyle) kodu **gönderim anında** basar, eski davetleri **aynı işlemde**
emekliye ayırır ve gönderir; kira 60 sn (gönderim bütçesi 10 sn + 5 sn audit'ten uzun) — iki pod aynı
daveti iki kez basamaz. **Kuyrukta kod yoktur** (şema + log taraması). Tavanlar ADR 0022 §9 ve EM-7C
notunun **bugünkü tenant tavanlarıdır**; panel ve kuyruk aynı sayaçları paylaşır — tavan isteği
reddettirmez, gönderimi geciktirir (plan §2 madde 11; ilk toplu senkron saatlere yayılır).

### 10. Askıya alma (ADR 0025 + K-26)

ADR 0025 askıyı bir **yazma kilidi** olarak tanımlar; okumalar, CSV ve kaydın telafi yolları askıyı
bilmez; panelde deaktivasyon askıda kapalıdır (ADR 0025 sınır 11). API aynısını uygular; tek fark
K-26'dır.

| Askıda **açık** | Askıda **403 `tenant_suspended`** |
|---|---|
| bütün `GET`'ler, `/v1/punches`, `/v1/directory` · webhook gönderimi (kuyruktaki teslimlerin işçi tarafından gönderilmesi) · `PUT /v1/employees` (**yeni** çalışan) · `resend-activation` · davet kuyruğu işçisinin kod basıp göndermesi · tetikleyicilerin olay, teslim ve izdüşüm yazması (askı dokunuşu durdurmaz — holding dokunuşu dahil) · **K-26 savunma yazmaları:** API anahtarı iptali, uç nokta devre dışı bırakma (panel) | `PUT /v1/locations` · var olan çalışanı değiştiren `PUT /v1/employees` (**`active:false` dahil**) · `DELETE /v1/employees` · anahtar oluşturma · uç nokta oluşturma / döndürme / adres değiştirme · test ping · panelden *"yeniden gönder"* |

**Tabloda adı geçmeyen her M11 yazması askıda kapalıdır** (varsayılan kapalı). Askı sütunları OP-15'e
bağlı → API-4 yalnız tek **kontrol noktasını** kurar; tablonun testleri API-4b'dir ve OP-15'e
bağlıdır.

### 11. Sandbox (K-3, K-28) ve panel yazma kapıları (GATE-1, K-37)

**Sandbox:**
- **Ayrı alan adı** — `taptime.mt`'nin alt alanı **değil**; `internal/handler/cookies.go:138-159`'un
  kısıtı hiç tetiklenmez, çerez geçişi gerekmez.
- **Ayrı derleme:** simülasyon kodu `internal/sandbox`'ta; onu **yalnız** `//go:build sandbox` tag'li
  bir `cmd/tappa` dosyası import eder. Kapılar: prod derlemesi için `go list -deps` **negatif**,
  sandbox için **pozitif** kontrol (`packaging_test` kalıbı — sembol testi `-ldflags=-s -w` yüzünden
  boş geçerdi); CI `-tags sandbox` ile de vet/test koşar; deploy kapısı (`verify-image.sh` derleme
  bilgisini zaten okuyor) imajın tag'ini doğrular. Sandbox ikilisi **yalnız** `TAPPA_ENV` = `sandbox`
  ile başlar (boş değerin dev'e düşmesi kapanır); tag'siz ikili `sandbox` değerini **reddeder**.
  Mevcut dev simülatörünün dört katlı kapısı **gevşetilmez**.
- **Sertleştirme (K-28):** `IsHardened()` = `prod` ∨ `sandbox`; dokuz `IsProd()` çağrısının dokuzu bu
  yükleme geçer (Bağlam tablosu); `EnvDev`'e bağlı üç kapı (`config.go:738`, `devtap.go:71`,
  `sun/mint.go:78`) sandbox'ta kapalı kalır.
- **Sanal plaket izin listesi:** sandbox minter'ı ve çözümleyicisi yalnız ayrılmış, NXP olmayan bir
  sanal UID aralığını kabul eder (gerçek NXP UID'si `0x04` ile başlar); kontrol **her çözümlemede ve
  her eklemede** koşar (`encoded_at`'a dayanılmaz — damgasız gerçek plaket meşru bir durumdur).
  Sandbox kendi anahtar ve KEK'leriyle kurulur; prod yedeği oraya geri yüklenmez.
- **Simülasyon yüzeyi:** yalnız sandbox'ta `POST /v1/sandbox/taps {employeeRef, locationRef,
  channel}`; simülasyon **holding dokunuşunu da** üretir.
- **Hesap açılışı:** sandbox kendi operatör yüzeyini kendi ops host'unda çalıştırır; `/signup`
  prod'daki gibi; API, webhook ve holding **yalnız sandbox operatörü açınca** (K-22) — operatörsüz
  anahtar 403.
- **E-posta:** gerçek e-posta yok; davet `panel` modunda.
- **Altyapı:** ayrı namespace + Postgres; NetworkPolicy (prod'unki gibi kendi namespace'ini korur),
  Pod Security `restricted`, `ResourceQuota`/`LimitRange`.

**GATE-1 (K-37)** — API-9'un önkoşulu: panelin dört `sameOrigin` kopyası (`adminlogin.go`,
`signup.go`, `adminreset.go`, `activate.go`) **her dalda** `Sec-Fetch-Site: same-site` ve
`cross-site`'ı reddeder — `Origin` olsa da olmasa da. **Diğer her durumda bugünkü davranış aynen:**
katı üç kopya ve Continue `Origin` yokken başlıksız/`none` isteği reddetmeye devam eder (login CSRF
kapısı, `adminlogin.go:1585-1592`); aktivasyon `Submit`'i kabul eder; `no-referrer` sayfaları `Origin:
null` + `same-origin` gönderir (ölçülmüş — `internal/httpx/operatorhost.go:53-57`) ve kabul edilmeye
devam eder. Aktivasyon `Submit`'inin `strict=false` dalı (`activate.go:578,1603-1605`) `Origin` yokken
de `Sec-Fetch-Site`'a bakar. Bütün başlık bileşimleri bir tablo testiyle pinlenir. Android rölesinin
(`android/…/HttpRelayServer.kt:17-19`) *"same-site"* yorumu güncellenir (röle `Origin` dalında kalır).

### 12. Pasifleştirme bütçesi (K-25)

`DELETE /v1/employees` ve `PUT {active:false}` aynı sınıftır ve **anahtar başına saatlik** bir bütçeyi
paylaşır — **varsayılan 20**. Bütçe **bellekte tutulmaz** (geri alınamaz bir eylemin tek freni yeniden
başlatmada sıfırlanamaz): son bir saatin audit satırlarından, **anahtar başına advisory kilit**
altında sayılır (`db/queries/invites.sql:403` emsali). **Kilit, sayım, pasifleştirme ve audit satırı
tek işlemdedir** ve audit `RecordTx` ile yazılır (`internal/domain/tenant/staff.go:72-91` emsali) —
audit kendi işlemini açsaydı ikinci istek kilidi ilk commit'te alıp henüz görünmeyen audit'i sayamaz,
N eşzamanlı istek bütçeyi eşzamanlılık derecesi kadar aşardı. Aşılınca **429** + `Retry-After`,
owner'a panelde görünür uyarı ve `api.deactivation_budget_exceeded` audit satırı. Owner'ın birden çok
anahtarı bütçeyi çarpar — kabul edildi; sızmış tek anahtar saatte en çok 20 hak alır. **ADR 0010
sapması:** iki adımlı onay API'de yoktur, yerini bütçe alır (ADR 0010 notu).

### 13. Ayrı host ve paket haritası (K-9)

- **Host:** `api.taptime.mt` (`TAPPA_API_HOST`); iki yönlü kapı — API host'unda HTML/panel rotaları
  404, ana host'ta `/v1` 404 (bugünkü `operatorHostOnly` genelleşir). Panel ve tap çerezleri host'a
  bağlı olduğu için API host'una gitmez; API'de çerez yoktur, CSRF yüzeyi yoktur.
- **Yeni paketler** (CLAUDE.md §3'e *"henüz yok — M11"* diye işlendi):
  - `internal/apikey/` — anahtar üretimi, biçim kontrolü, HMAC; çözümleyiciyi `internal/db` üzerinden
    çağırır, sonucu (*"taşı, uygulama"*) değerlendirir. Log yok, sır döndürmez.
  - `internal/handler/api/` — `/v1` yüzeyi (ayrı host, JSON hata, gövde sınırı, Idempotency, askı
    kontrol noktası, `openapi.yaml`). Müşteri paneli bu paketi import etmez (`internal/handler/operator/`
    emsali).
  - `internal/webhook/` — teslim işçisi (kiralama, geri çekilme), imza, K-19'un süreç içi tenant kümesi
    (davet kuyruğu işçisi aynı kümeyi tüketir; arayüz tüketici tarafında — CLAUDE.md §7).
  - `internal/egress/` — SSRF-güvenli HTTP istemcisi: §7'nin izin listesi ve dial-time kapısı, yalnız
    stdlib (`net`, `net/netip`, `net/http`); tek kullanıcısı `internal/webhook`. Ayrı paket, çünkü egress
    açıkken tek savunma odur ve kendi tablo bazlı testlerini (her IANA kaydı) taşımalıdır.
  - `internal/sandbox/` — simülasyon; **yalnız** `sandbox` tag'li `cmd` dosyası import eder (§11).
  - Holding erişimi ayrı paket değildir: `internal/db/holding.go` (ADR 0027 §2).
  - Davet kuyruğu işçisi mevcut `internal/invite` paketinde genişler.

## Güvenlik değerlendirmesi

- **Kimlik:** anahtar yalnız HMAC'iyle saklanır (veritabanı dökümü anahtar vermez); biçim kontrolü ve
  IP kovası çözümleyiciden önce (sel veritabanına ulaşmaz); çözümleyici md.7'nin beş kısıtıyla en çok
  bir satır döner; iptal ve K-22 kapısı önbelleksiz, her istekte. Anahtarın tenant'ı **tek** yetki
  kaynağıdır — istemcinin seçtiği bir tenant yoktur; `ref` aramaları `WithTenant` + açık filtreyle.
- **Tenant izolasyonu (CLAUDE.md §4.5):** API, olay tabloları ve teslimler hedef tenant'ta, RLS
  beşlisiyle; tenant sınırını aşan tek okuma rehberdir ve o ADR 0027'nin kapalı listesindeki
  `holding_directory`'dir. C'nin anahtarı A'yı göremez (API-3, API-7, API-8 kabulü).
- **Kayıt kaybı (CLAUDE.md §4.6):** akışa girmeyen satırlar (flag, reject, manuel) **yazılmaya devam
  eder**; akış yalnız bir izdüşümdür. Tetikleyici hatası kaydı geri alır — adlandırılmış bedel (§4).
- **Değişmezlik (CLAUDE.md §4.3):** `punch_events` append-only; `version`/`voided` v1'de üretilmez;
  düzeltme ADR 0011'in yolundan.
- **Sırlar (CLAUDE.md §4.7, §7):** imza anahtarı kendi KEK'iyle mühürlü, bir kez gösterilir; API
  anahtarı ve imza anahtarı önekli (R7d); `Authorization`, URL ve `*url.Error` log'a asla.
- **SSRF:** §7 — tek savunma; kayıttan türetilmiş izin listesi, dial anında.
- **CSRF:** API host'unda çerez yok; panelin yazma kapıları GATE-1 ile sertleşir.
- **Sandbox:** prod derlemesinde simülasyon **kodu yoktur** (derleme düzeyinde); gerçek UID reddi her
  çözümlemede; sertleştirme prod'la aynı.
- **Geri alınamaz eylem:** pasifleştirme bütçesi DB'den, tek işlemde, eşzamanlılıkta tam 1.

## Elenen seçenekler

| Seçenek | Neden elendi |
|---|---|
| **Olayı Go katmanında yazmak** | Eski binary ve geri dönüş olay doğurmazdı; her yazma yolunun (tap, onay, gelecekteki yollar) hatırlaması gerekirdi |
| **`xid8` imleci** | Commit sırası ile görünürlük ayrışır (1.–2. tur); dışarıya verilen imleç satır atlardı |
| **Küresel dizi (`CACHE 20`)** | 2. tur: kilit altında bile 1234 olayın 1016'sı kaçtı; tenant'lar arası hacim sızdırır |
| **Yalnız INSERT REVOKE** (`arwd`'e güvenmek) | Prod yeni tabloyu dört fiille doğurur; CI göremez (§1) |
| **Tetikleyici fonksiyonunda PUBLIC EXECUTE** | 4. tur: `tappa_app` geçici tabloya bağlayıp reddedilmiş bir kayıt için olay yazdırabiliyordu |
| **SSRF yasak listesi / Go'nun `IsGlobalUnicast`'ı** | Yasak listesi eksik kalır; `IsGlobalUnicast` NAT64/IPv4-uyumlu biçimlere `true` der — izin listesi + IANA kaydı |
| **Sandbox = `sandbox.taptime.mt` + `TAPPA_DEV_TOOLS`** (1. sürüm) | Kurulamaz çıktı (dev kapıları dört katlı ve dev'e bağlı); alt alan + çerez geçişi (6. sürüm) maliyeti büyüdü — K-3 |
| **Bellekte pasifleştirme bütçesi** | Yeniden başlatmada sıfırlanır; geri alınamaz eylemin freni olamaz |
| **Idempotency *"işleniyor"* satırı ayrı işlemde** | Süreç ölünce anahtar takılı kalırdı |
| **`resend-activation`'ın istek anında eski daveti öldürmesi** | Resend döngüsü çalışanı süresiz kodsuz bırakabiliyordu (2. tur) |
| **Uç nokta imza anahtarını *"göster"*** | Ele geçirilmiş oturuma sahte imzalı punch (K-12) |
| **İmzaya ek zaman damgası başlığı** | İmzaya girmeyen başlık sahte güven verir (K-11) |
| **Tenant'lar arası tarayan işçi** | CLAUDE.md §4.5'e yeni bir istisna olurdu (K-19) |

## Karar verilmedi / kapsam dışı (v1 — plan §8)

Punch yazma API'si · punch iptali/düzeltmesi · departman uçları · lokasyon silme API'si · webhook
yönetimi API'den · lokasyon başına saat dilimi · KF'nin KM çalışanı için onay vermesi · ikiden fazla
tenant'lı holding için ek UI · OAuth · paylaşımlı oran deposu · egress NetworkPolicy.

## Test edilecekler (adlarıyla)

Henüz yazılmamış testlerin adları `Test` öneki olmadan verilir (`TestEveryNamedTestExists`
kuralı: belgede yalnız var olan testin adı yazılır); test yazıldığında önek eklenir ve bu satır
gerçek adla güncellenir. Önekli adlar var olan testlerdir.

Adlar önerilir; uygulama görevi kart düzeltmesiyle değiştirebilir, kapsamı daraltamaz. *(Kaynak:
plan §5 kabul çekirdeği.)*

**GATE-1** — `TestSameOrigin_RefusesSameSiteAndCrossSiteOnEveryBranch` (dört kopya, `Origin` var/yok,
aktivasyon `strict=false` dahil) · `TestSameOrigin_EveryOtherHeaderCombinationIsUnchanged` (tablo
testi; katı kopyalarda `Origin` yokken başlıksız/`none` RED) · `TestSameOrigin_NoReferrerPagesStillPass`
(`Origin: null` + `same-origin`) · mevcut panel/kayıt/sıfırlama/aktivasyon/röle akış testleri yeşil.

**API-1** *(yazıldı — "API-1 uygulama notu")* — `TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd` ·
`TestEmployeeEmail_UniqueAmongActiveOnly` (kısıt adı korunur; pasifle aynı e-postada yeni aktif açılır,
iki aktif açılamaz) · `TestEmployeeUpdatedAt_OnlyDirectoryColumnsOnEveryUpdatePath` (dört yol + panelin
external-ref yazması) · `TestEmployeeLocationReaders_Counted` · `TestTenantApiColumns_AppCannotWrite`.

**API-2** — (a) `Feed_PlainInsertIntoTransactionsBearsAnEvent` · (b) `Feed_StressNoMissNoDeadlock`
(≥ 12 yazıcı, ≥ 3 tenant, ters yönlü holding dokunuşları, abort/savepoint karışık, eşzamanlı okuyucu;
`seq` yoğun) + mutasyonlar `FeedMutant_UnlockedSequenceMisses`,
`FeedMutant_CounterInSeparateTransactionMisses`, `FeedMutant_TwoTriggersLockingCountersDeadlock` ·
(c) `RestoreVerify_CountersAheadOfEvents` · (d) `Feed_FlagApprovalManualPractice` ·
(e) `Feed_OutsiderSeesNothing` · (f) `FeedTriggers_RefuseVerbsUnderArwdAndTempTableBinding`
(iki definer) · (g)–(i) `FeedTrigger_ErrorRollsBackTheRecord`, `FeedTrigger_SavepointKeepsCounterAndEventTogether`,
`FeedTimeouts_ShownAtStartup` · (j) `Feed_TargetRowsAndSnapshots` (iki hedef satırı holding
tetikleyicisi yazar; kendi-tenant tetikleyicisi `site_*` satırına ve onayına hiç olay yazmaz; işveren
olayı tam bir kez) + `WebhookDeliveries_KMatKFIsTwo` · (k) `Feed_ClosedApiWritesEventsButNoDeliveries` ·
(l) `FeedBackfill_WithinTwoSeconds` · (m) `FeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid`
+ K-34'ün `webhook_endpoints.url`/`signing_key_sealed` pinleri + yazılan dört tablonun sütun pini ·
(n) `Restore_FreshPodCarriesTheFeedRole` · (o) `Feedwriter_EveryGrantIsIndividuallyNecessary` ·
(p) `Feed_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce` · (q)
`Feed_MissingCounterRowsNoDeadlock` + `RestoreVerify_EveryTenantHasACounter`.

**API-3** — `ApiKeysCatalog` · `ApiKeyRevocation_IsOneWay` · `ApiKeyRevocation_ConcurrentAndBulkAreSafe` ·
`ApiKeyAuth_RevokedIs401ClosedIs403` · `ApiKeyAuth_WrongPrefixNeverReachesTheDatabase` ·
`ApiKeyAuth_NothingLogged` ·
`Redline_R4R7R7bR7dCatchDeliberateViolations` ·
`Config_EqualEnvKeysRefuseToStart` · `ApiKeyAuth_OutsiderKeyCannotSeeAnotherTenant`.

**API-4 / API-4b** — `ApiCatalog_Idempotency` · `ApiHost_TwoWayGate` (API host'unda `/admin`
404, ana host'ta `/v1` 404) · `ApiAuth_IPBucketBeforeResolver` · `Idempotency_SameKeyDifferentBodyIs422` ·
`Idempotency_ConcurrentSameKeyOneEffect` · `Idempotency_DeathMidRequestLeavesNoStuckKey` ·
`ApiSuspension_EveryCellOfTheTable` (API-4b, OP-15'e bağlı).

**API-5** — `LocationsPut_OutsiderKeyCannotTouchAnotherTenant` · `LocationsPut_DifferentTimezoneIs422` ·
`LocationsPut_RepeatOpensNoRow`.

**API-6** — `EmployeesPut_ActiveEmailElsewhereIs409` · `EmployeesPut_InactiveToActiveOpensANewRow` ·
`EmployeesPut_EmailChangeKillsOldInviteInSameTransaction` · `ResendActivation_LoopNeverKillsTheLiveInvite` ·
`ResendActivation_ActivatedIs409` · `InviteQueue_HoldsNoCode` · `InviteQueue_TwoPodsMintOnce` ·
`DeactivationBudget_ComesFromTheDatabase` · `DeactivationBudget_ConcurrentLastSlotExactlyOne`
(`-race`) · `DeactivationBudget_ExceededIs429WithAlertAndAudit` · `EmployeesGet_NoEmail`.

**API-7** — `Punches_FourShapesTimesTwoScopes` (anlık görüntü alanlarıyla, doğru tenant'a) ·
`Punches_OutsiderSeesNone` · `Punches_NoManualNoRejected` · `Punches_Limit1001Is400`.

**API-8** — `Directory_HoldingTenantsOnlyNoEmailWithActive` · `Directory_Paged` ·
`Directory_OverlapWindow` · `Directory_OwnStaffAlwaysFromTheRLSQuery` ·
`Directory_KeyOfATenantWithoutHoldingSeesOnlyItsOwnStaff` (holding'siz anahtar: kendi
çalışanları gelir, `holding_directory` 0 satır, başka tenant yok).

**API-9** — `WebhookEndpointCatalog` · `WebhookEndpoint_OnePerTenant` ·
`WebhookEndpoint_KeyShownOnce` · `WebhookPing_SharesTheResendBucket` ·
`WebhookEndpoint_ClosedApiRefused`.

**API-10** — `WebhookRouting_FourShapes` · `Egress_RefusesEveryIANASpecialPurposeEntry` ·
`Egress_DNSRebindingSecondAnswerRefused` · `Egress_IPv6OnlyGlobalUnicastMinusCarveOuts` ·
`Egress_NodeAndIngressAddressesRefused` · `Egress_EmptyNodeListRefusesToStart` ·
`Egress_ProxyEnvironmentIgnored` · `Egress_RedirectNotFollowed` ·
`WebhookWorker_DisabledEndpointGetsNothing` · `WebhookWorker_TwoPodsSendOnce` ·
`WebhookWorker_NoOpenTransactionDuringHTTP` · `WebhookWorker_DrainsWithinShutdownBudget` ·
`WebhookSignature_KnownAnswer` · `WebhookWorker_URLAndURLErrorNeverReachLogOrAudit`
(CLAUDE.md §7: başarısız gönderimlerde — DNS, TLS, zaman aşımı, ret — süreç log'unda ve audit
`detail`'inde uç noktanın adresi ve `url.Error` metni yok; yalnız uç nokta kimliği ve hata sınıfı).

**API-11** — `Packaging_ProdBuildHasNoSandboxPackage` · `Packaging_SandboxBuildHasIt` ·
`Config_UntaggedBinaryRefusesSandbox` · `Sandbox_RealNXPUIDRefusedOnEveryResolveAndInsert` ·
`Config_IsHardenedCoversTheNineGates` · `Sandbox_KeyWithoutOperatorIs403` ·
`Sandbox_SimulatesAHoldingTap` · **CI kapısı:** iş akışı `go vet -tags sandbox ./...` ve
`go test -tags sandbox ./...`'i de koşar (`CI_RunsVetAndTestWithTheSandboxTag` — iş akışı
dosyasını okuyan bir paketleme testi) · **deploy kapısı:** `verify-image.sh` imajın derleme bilgisindeki
tag'i doğrular — sandbox imajı `sandbox` tag'siz, prod imajı tag'li ise reddedilir
(`VerifyImage_RefusesAWrongBuildTag`).

**API-12** — `OpenAPI_EveryRouteAndErrorDocumented` · `Guide_CarriesAll31ContractDifferences`.

**API-13** — runbook: spec §7'nin dokuz maddesi; `tappa-security-auditor` tam tur ONAY; operatörden
K-22 (`KF`/`KM`) ve KF+KM holding'i; K-18 bağlama; prod anahtar çiftleri ve `pg_auth_members`;
holding dışı izolasyon prod'da örneklenir.

## Sonuçlar

- **CLAUDE.md** güncellendi (bu ADR ile aynı değişiklikte): §3 (yeni paketler, *"henüz yok — M11"*),
  §6 (kalıcı `arwd` kuralı), §7 (log yasakları: API anahtarı, `Authorization` başlığı, webhook imza
  anahtarı, webhook URL'si, `*url.Error` metni).
- **Notlar** (her birinin sonunda *"M11 notu (2026-10-11)"*): ADR 0002 (md.7 — yedinci çözümleyici ve
  `tappa_resolver`'ın `tenants(id, api_enabled_at)` yetkisi), 0010 (K-25 sapması, K-24 yan etkisi),
  0021 (`api_enabled_at`/`api_code`'u yazan `op_*`; `prosecdef` sahip testi iki rolle), 0022 §7
  (otomatik davet + kuyruk + tavanlar), 0025 (§10 tablosu + K-26).
- **Uygulama sırası** (plan §5): GATE-1 · API-1 → API-3 → API-4 (→ API-4b, OP-15) → API-5, API-6 ·
  API-1c → API-2 → API-7 · API-1a → API-8 · GATE-1 + API-2 + API-4 → API-9 → API-10 → API-11 → API-12 →
  API-13. Her görev yapıcı → üçüncü göz; CLAUDE.md §4'e değenlerde (GATE-1, API-2, API-3) ayrıca
  `tappa-security-auditor`.

## API-1 uygulama notu (2026-10-11)

Şema üç migration'dır; numaralar `00037`'den başlar (`00036` OP-15'e ayrıldı):

- **`00037`** — `employees`: `external_ref`, `employee_code`, `first_name`, `last_name`, `updated_at`;
  `location_id` NULL olabilir, `employees_home_or_external_ref` CHECK'i ile (K-14); e-posta indeksi
  `employees_tenant_email_key` **adıyla** yeniden kuruldu, `status <> 'deactivated'` süzgeciyle (K-24);
  `employees_tenant_external_ref_key` (canlı satırlar arasında tekil). `updated_at`'ı yalnız
  `employees_directory_clock` tetikleyicisi (`tappa_employees_touch_updated_at`, `EXECUTE` yalnız
  sahipte, `TG_RELID` assert'li) yazar: INSERT'te ve rehberin gördüğü sütunlardan biri (ad, `first_name`,
  `last_name`, `full_name`, kod, `external_ref`, durum) değişince `clock_timestamp()`; başka her UPDATE'te
  eski değer korunur, elle yazılan değer ezilir. `full_name` listeye **eklendi**: panelin açtığı satırda
  ad/soyad yoktur ve rehberin gösterebileceği ad odur. `locations`: `external_ref` (tenant içinde tekil),
  `active` (K-20 etiketi). `invite_requests` (K-17): anahtar `(tenant_id, employee_id)` — kişi başına tek
  canlı istek; `tappa_app`: SELECT, INSERT `(tenant_id, employee_id)`, UPDATE `(lease_until, attempts)`,
  DELETE (gönderilen istek kuyruktan çıkar — §9'un beş sütununda "gönderildi" durumu yok). External ref
  biçimi URL yolunda taşındığı için RFC 3986 "unreserved" kümesidir, harf/rakamla başlar, en çok 64
  karakter (`^[A-Za-z0-9][A-Za-z0-9._~-]{0,63}$`); `employee_code` ve iki ad kontrol karakteri ve
  kenar boşluğu taşımayan, sınırlı etiketlerdir. API-5/6 bu biçimleri 422 ile söyler.
- **`00038`** — `tenants.api_enabled_at`, `tenants.api_code` (`^[A-Z][A-Z0-9]{1,15}$`, **tekil değil**:
  tenant'lar arası tekillik yazmayı tenant-ötesi bir eyleme çevirirdi — 00024'ün `vat_number`
  gerekçesi) ve `tenants_api_needs_a_code`. Ön koşul `tappa_app`'in `tenants` üzerinde tablo düzeyi
  INSERT/UPDATE tutmadığını, son koşul iki sütunu yazamadığını ölçer.
- **`00039`** — tek yazar: `op_enable_tenant_api(oturum, tenant, kod)` (açılış anı ilk açılışta kalır,
  kod değişir) ve `op_disable_tenant_api(oturum, tenant)` (`api_enabled_at` NULL, kod **kalır** — punch'lar
  onu `company` olarak taşımaya devam eder). ADR 0021 sözleşmesi: `void`, birincil anahtarla `FOR NO KEY
  UPDATE`, K6 tenant satırı (`tenant.api_enabled` `{actor_kind, api_code}` / `tenant.api_disabled`)
  `clock_timestamp()` ile, operatör satırı (`tenant_api_enabled` / `tenant_api_disabled`) her kabul
  edilen çağrıda; bilinmeyen tenant aynı `void`. OP-15'in dalda bekleyen çiftinin (suspend/reinstate)
  biçimi emsal alındı; okumaya bağlama (OP-16 (b2)) yok — çağrı kayıtlı bir değeri bir adayla
  karşılaştırmaz. `op_begin_read` ve `op_read_audit` 00034 gövdeleri + iki tür adıyla değiştirildi. Ön
  koşul tür CHECK'ini ve `tenant_write_shape`'i **tam metinle**, değiştirdiği iki gövdeyi `md5(prosrc)` ile
  karşılaştırır: OP-15'in `00036`'sı önce birleşirse bu dosya sessizce onu geri almaz, "rebase" der.

**`arwd` kuralının mekaniği (§1 madde 4).** Taze bir hacimde, ilk migration'dan önce üretimin
`pg_default_acl`'i kurulur: CI'da, yeni geliştiricide ve `make db-reset`'te aynı init dosyası
(`scripts/db-init/03-dev-production-default-acl.sql`; ilk hali `ci.yml`'de ayrı bir adımdı). 00035'teki
iki veritabanı karşılaştırıldı — dar ve geniş yapımlar arasında uygulama tablolarında **fark yok**, tek
fark `goose_db_version`'ın UPDATE/DELETE'i. O tablo ilk migration'dan önce doğduğu için katalog testinin
tanığıdır: tanık dar ise test CI'da kırmızıdır, başka yerde (init dosyasından eski bir hacim) atlanır —
geçiş sayılmaz — ve yeniden kurmayı söyler. Test, 00035'teki
ilişki/sütun/fonksiyon/dizi kümesini **dondurur** ve ondan sonra doğan her nesnenin rolü rolüne matriste
olmasını ister (`m11Tables`, `m11Columns`, `m11Functions` — `internal/db/m11schema_test.go`).
*Adlandırılmış yan bulgu:* üretimde `tappa_app`, `goose_db_version` üzerinde UPDATE ve DELETE tutuyor
(göç defteri uygulama rolüyle değiştirilebilir) — kapsam dışı, API-13 runbook'una aday.

**`opAtVersion`.** Operatör testlerinin eski bir sürüme inmek için çalıştırdığı Down zinciri `00037` ve
`00038`'i **atlar** (`opAtVersionSkips`): ikisi operatör nesnesine dokunmaz
(`TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject`), Down'ları `employees`/`tenants` üzerinde
ACCESS EXCLUSIVE alır ve `00037`'ninki, ev lokasyonu olmayan ya da pasif/aktif aynı adresi paylaşan
satır varsa (K-14/K-24 testleri bunları commit eder) bilerek reddeder. `00039`'un Down'ı yürütülür.

