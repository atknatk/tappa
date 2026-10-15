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
             recorded_at, channel, direction, employee_id, employee_ref, employee_code, employee_company,
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
  `recorded_at` = sunucunun yazdığı an. **`direction`** (`in`/`out`, NOT NULL — **K-40, kullanıcı kararı
  2026-10-14**): Tappa'nın **işverende** hesapladığı yön, `transactions.type`; onay olayında ana kaydın
  yönü; holding punch'ının iki satırı da işverenin yönünü taşır. Yönü olmayan satır (ham yazıcı ya da
  M4-04 öncesi bir kayıt — ürün her `ok` ve `flag` satırına yön yazar) punch değildir, olay yazılmaz.
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
  *(API-2'de yeniden ölçüldü — `TestFeedwriter_EveryGrantIsIndividuallyNecessary`: `transactions.created_at`
  gereksiz çıktı ve verilmedi (`recorded_at` tetikleyicinin `now()`'ıdır); `USAGE`'ın tek başına
  zorunluluğu PUBLIC'in `USAGE`'ı alınarak ölçüldü — "API-2 uygulama notu".)*

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
geri doldurma tenant başına küçük işlemlere bölünür. *(API-2'de aşıldı — 700 000 satırda 5,2 sn —
ve bölündü: `00049` tetikleyicileri kısa kilitle kurar, `00050` geçmişi tenant başına, **keyset**
pencereleriyle en çok 1 000 punch'lık işlemlerde, her işlemde yalnız o tenant'ın sayaç satırını tutarak
yazar; "API-2 uygulama notu", ikinci tur.)*

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

**Takılı yazıcı** *(API-2'de uygulandı: havuz parametreleri `internal/db/feedtimeouts.go`, açılışta
okunup doğrulanır — `TestFeedTimeouts_ShownAtStartup`)* (ölçüldü: tenant'ın 8 yazıcısının 8'i bekledi; holding'de KF'deki takılı işlem KM'yi
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

**API-2** *(yazıldı — "API-2 uygulama notu"; adlar önerilenle aynı, önek eklendi; ikinci turun ekleri
satırın sonunda)* —
(a) `TestFeed_PlainInsertIntoTransactionsBearsAnEvent` · (b) `TestFeed_StressNoMissNoDeadlock`
(12 yazıcı × 100, 4 tenant, ters yönlü holding dokunuşları, abort/savepoint karışık, tenant başına
eşzamanlı okuyucu; `seq` yoğun) + mutasyonlar (laboratuvar şemasında — not) `TestFeedMutant_UnlockedSequenceMisses`,
`TestFeedMutant_CounterInSeparateTransactionMisses`, `TestFeedMutant_TwoTriggersLockingCountersDeadlock`
ve kontrolü `TestFeedMutant_LabControlPasses` ·
(c) `TestRestoreVerify_CountersAheadOfEvents` · (d) `TestFeed_FlagApprovalManualPractice` ·
(e) `TestFeed_OutsiderSeesNothing` (+ K-33 mutantları olay dalında: `TestHoldingFilter_MutantsM1M2M3AreRedInFunctionsAndTriggers`) ·
(f) `TestFeedTriggers_RefuseVerbsUnderArwdAndTempTableBinding` (iki definer) · (g)–(i)
`TestFeedTrigger_ErrorRollsBackTheRecord`, `TestFeedTrigger_SavepointKeepsCounterAndEventTogether`,
`TestFeedTimeouts_ShownAtStartup` (+ ürün yolunda sayaç beklemesi: `TestFeedCounter_LockWaitIsWrittenOnceMore`,
`TestFeedCounter_HoldingTapLockWaitIsWrittenOnceMore`, `TestFeedCounter_LockHeldThroughBothWaitsIsTheNamedCost`,
`TestFeedCounter_RowsThatAreNoEventAreWrittenPlainly`) · (j) `TestFeed_TargetRowsAndSnapshots` (iki hedef
satırı holding tetikleyicisi yazar; kendi-tenant tetikleyicisi `site_*` satırına ve onayına hiç olay
yazmaz; işveren olayı tam bir kez) + `TestWebhookDeliveries_KMatKFIsTwo` ·
(k) `TestFeed_ClosedApiWritesEventsButNoDeliveries` · (l) `TestFeedBackfill_WithinTwoSeconds` ·
(m) `TestFeedCatalog_TwoRolesTriggerOrderExecuteOwnerOnlyRelid` (K-34'ün `webhook_endpoints.url`/`signing_key_sealed`
pinleri ve yazılan dört tablonun sütun pini dahil; ayrıca `TestHoldingDefiner_NeverSelectColumns`,
`TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd`, `TestOperator00026_ReverseCatalogPin`) ·
(n) `TestRestore_FreshPodCarriesTheFeedRole` · (o) `TestFeedwriter_EveryGrantIsIndividuallyNecessary` ·
(p) `TestFeed_ApprovalAfterExitWritesEmployerEventAndDeliveryOnce` · (q)
`TestFeed_MissingCounterRowsNoDeadlock` (+ mutantı `TestFeedMutant_PreLockWithoutMakeExistDeadlocks`) +
`TestRestoreVerify_EveryTenantHasACounter` · ek: `TestFeed00048And00049_DownGiveBackAndUpTakesItAgain`,
`TestRoleGate_MemberOfAnyRoleIsRefused`. **İkinci tur (2026-10-14):** K-40 yön —
`TestFeed_DirectionIsTheEmployers` (dört şekil + onaylı flag; mutantlar: ters yazılan ve yazılmayan yön),
`TestFeedDirection_NightShiftInThenOutAcrossMidnight`, `TestFeed_HoldingFlagApprovalManualReject`;
büyük tenant'ın geri doldurması — `TestFeedBackfill_BigTenantLiveTapsLoseNothing`; tek sayaç kümesi —
`TestFeed_OneCounterSetPerTransaction`, `TestFeed_TwoEventRowsInOneTransactionAreRefusedNotDeadlocked`,
`TestFeedMutant_TwoEventRowsWithoutTheCheckDeadlock`; dökümün sayaçları —
`TestPgBackup_ManifestCarriesTheDumpsFeedCounters` (+ `TestRestoreVerify_CountersAheadOfEvents`'in iki ardışık
geri yükleme vakası). **Üçüncü tur:** planın metne bağlılığı — `TestFeedBackfill_ThePlanIsTheTexts`; geri
doldurmanın yönü — `TestFeedBackfill_WithinTwoSeconds` ve `TestFeedBackfill_BigTenantLiveTapsLoseNothing`
(karışık in/out); işaretin yalnız tetikleyicinin olması — `TestRedline_TheCounterSetMarkIsTheTriggersAlone`;
tenant'sız veritabanının geri yüklemesi — `TestRestoreVerify_FeedManifestGate` (+
`TestRestoreVerify_EveryTenantHasACounter`'ın sıfır tenant vakası).

**API-3** *(yazıldı — "API-3 uygulama notu"; adlar önerilenle aynı, önek eklendi)* — `TestApiKeysCatalog`
(+ `TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd`'in `api_keys` satırı ve iki fonksiyonu,
`TestRLS_ApiKeys_AnotherTenantsKeysAreInvisible`, `TestApiKeys00051_DownGivesBackAndUpTakesItAgain`) ·
`TestApiKeyRevocation_IsOneWay` · `TestApiKeyRevocation_ConcurrentAndBulkAreSafe` ·
`TestApiKeyAuth_RevokedIs401ClosedIs403` · `TestApiKeyAuth_WrongPrefixNeverReachesTheDatabase` ·
`TestApiKeyAuth_NothingLogged` (+ panel log'u: `TestApiKeyPanel_TheKeyIsShownOnceAndNeverAgain`) ·
`TestRedline_R4R7R7bR7dCatchDeliberateViolations` ·
`TestConfig_EqualEnvKeysRefuseToStart` · `TestApiKeyAuth_OutsiderKeyCannotSeeAnotherTenant`. Ekler:
`TestApiKeyTouch_AtMostOncePerMinute`, `TestApiKeyPanel_OnlyAnActiveOwnerOfThisBusiness`,
`TestApiKeyPanel_ManagerIs403EverywhereAndLeavesATrail`, `TestApiKeyPanel_AnotherBusinessCannotRevokeByGuessingTheID`,
`TestApiKeyPanel_CrossSiteWritesAreRefusedBeforeTheResolver`, `TestApiKeyPanel_AClosedAPIOffersNoCreateAndStillRevokes`,
`TestKeyedHash_KnownAnswer`, `TestShape_RefusesEveryOtherForm`, `TestPrincipal_AttributesAnAPIActToTheKey`.

**API-4 / API-4b** *(API-4 yazıldı — "API-4 uygulama notu"; adlar önerilenle aynı, önek eklendi)* —
`TestApiCatalog_Idempotency` (+ `TestM11Catalog_EveryNewTableMatchesTheMatrixUnderArwd`'in `api_idempotency` satırı,
`TestRLS_ApiIdempotency_AnotherTenantsRowsAreInvisible`, `TestApiIdempotency00052_DownGivesBackAndUpTakesItAgain`) ·
`TestApiHost_TwoWayGate` (API host'unda `/admin` 404, ana host'ta `/v1` 404) · `TestApiAuth_IPBucketBeforeResolver` ·
`TestIdempotency_SameKeyDifferentBodyIs422` · `TestIdempotency_ConcurrentSameKeyOneEffect` ·
`TestIdempotency_DeathMidRequestLeavesNoStuckKey` · `ApiSuspension_EveryCellOfTheTable` (API-4b, OP-15'e bağlı —
yazılmadı). Ekler: `TestIdempotency_LazyDeleteIsThisTenantsAlone`, `TestApiIdempotency_OverHTTP`,
`TestApiSurface_RevocationAndClosingTakeEffectOnTheNextRequest`, `TestApiSurface_CKeyCannotSeeAnotherTenant`,
`TestChain_ResolverAnswersAreTheContract`, `TestChain_NothingOfTheKeyReachesALogOrAResponse`,
`TestRequestID_AKeyShapedIDIsReplaced`, `TestBody_The64KiBBound`, `TestLimit_DefaultAndTheBound`,
`TestErrors_TheCodeSetIsSection8`, `TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8`,
`TestWrite_ACommitWithoutTheCheckpointIsRefused`, `TestWrite_IdempotencyKeyThroughHTTP`,
`TestPackage_ImportsNoPanelAndNoPanelImportsIt`, `TestLoad_APIHostIsOptionalAndOneSpelling`,
`TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker`; K-19 —
`TestWorker_NoticedTenantsReachEveryJob`, `TestWorker_DrainsWithinTheShutdownBudget`,
`TestWorker_TwoProcessesBothRunTheJob`, `TestWorker_NoTenantWideRead`,
`TestK19_ARecordedTapNoticesItsTenantsAndARefusalNobody`.

**API-5** *(yazıldı — "API-5 uygulama notu"; adlar önerilenle aynı, önek eklendi)* —
`TestLocationsPut_OutsiderKeyCannotTouchAnotherTenant` · `TestLocationsPut_DifferentTimezoneIs422` ·
`TestLocationsPut_RepeatOpensNoRow`. Ekler: `TestLocationsPut_CreatesUpdatesAndAnswersUnchanged`,
`TestLocationsPut_APanelBoundVenueIsUpdatedNeverMatchedByName`, `TestLocationsPut_IdempotencyReplay`,
`TestLocationsPut_AHoldingPeersVenueIsNeitherSeenNorChanged`, `TestLocationsPut_SuspendedAndClosedWriteNothing`,
`TestLocationsPut_TheBodyAndTheRefContract`, `TestLocationsPut_AVenueWithoutEvidenceRecordsATapAsFlag`,
`TestLocationsPut_ActiveIsALabelTheTapNeverReads`, `TestAPIWiring_ThePutOfALocationRunsThroughTheShippedSurface`;
ikinci tur: `TestInsertSelectAndDoUpdate_TheBeltReadsThem` (+ adın gizli karakter kuralı `TestVenues_TheNameBoundIsEnforcedBeforeTheColumn`
ve `TestParseName_RefusesRatherThanTruncates`'te); üçüncü tur: `TestVenueSave_ANameOnFileStaysEditableAndANewHiddenOneIsASentence`,
`TestLocationsPut_TheNameRuleJudgesOnlyWhatChanges`, `TestInsertSelectWrites_TheArmFiresAndStaysQuiet` (altı kopyada);
dördüncü tur: `TestNameHasHiddenCharacters_JoinersAndTagsOnlyWhereANameUsesThem`, `TestValidateVenues_CleansLikeTheVenueEditor`.

**API-6** *(yazıldı — "API-6 uygulama notu"; adlar önerilenle aynı, önek eklendi)* —
`TestEmployeesPut_ActiveEmailElsewhereIs409` · `TestEmployeesPut_InactiveToActiveOpensANewRow` ·
`TestEmployeesPut_EmailChangeKillsOldInviteInSameTransaction` (işçi tam basarken adres değişir — `internal/invite`) ·
`TestResendActivation_LoopNeverKillsTheLiveInvite` · `TestResendActivation_ActivatedIs409` · `TestInviteQueue_HoldsNoCode` ·
`TestInviteQueue_TwoPodsMintOnce` (`-race`) · `TestDeactivationBudget_ComesFromTheDatabase` ·
`TestDeactivationBudget_ConcurrentLastSlotExactlyOne` (`-race`; `internal/domain/tenant`) ·
`TestDeactivationBudget_ExceededIs429WithAlertAndAudit` · `TestEmployeesGet_NoEmail`. Ekler:
`TestEmployeesPut_CreatesQueuesAndNoticesAfterTheCommit`, `TestEmployeesPut_TheBodyContract`,
`TestEmployees_SuspensionClassesAreSection10`, `TestEmployees_IdempotencyReplay`,
`TestEmployees_OutsiderKeyCannotTouchAnotherTenant`,
`TestEmployees_NothingOfTheAddressOrTheKeyReachesALogOrATrail`,
`TestEmployeesPutDomain_EmailChangeRetiresInTheSameTransaction`, `TestEmployeesPutDomain_LinkRehireAndNames`,
`TestEmployeesPutDomain_ConcurrentNewRefOpensOneRow`,
`TestInviteQueue_LimitsDelayAndNeverRefuse`, `TestInviteQueue_OnlyAnInvitedPersonIsSent`,
`TestInviteQueue_TheLinkActivatesThroughTheWizardsPath`, `TestWorker_ALaterTenantIsNoticedAgainAndIsNoFailure`,
`TestAPIWiring_AnEmployeeRunsThroughTheShippedSurfaceAndTheQueue`, `TestApiKeyPanel_TheDeactivationBudgetAlertIsShownToTheOwner`;
genişleyenler: `TestEmployeeUpdatedAt_OnlyDirectoryColumnsOnEveryUpdatePath` (API'nin ad yazması),
`TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker` (çalışan alanı, kuyruk, işçinin başlama sırası). İkinci tur:
`TestInviteQueue_ACeilingDelaysAndSpendsNoTry`, `TestInviteQueue_GivingUpRetiresTheUnsentLinkAndSaysUndelivered`,
`TestAuditLog_TheAppCannotWriteTheTime`, `TestAuditLog00053_DownGivesBackAndUpTakesItAgain`,
`TestStaffAdd_TheNameRuleIsTheVenueRule`, `TestEmployeeAddDB_AHiddenCharacterIsASentence`,
`TestPanelHome_TheKeyBudgetBannerIsTheOwnersAlone`, `TestKeyBudgetBanner_AbsentAddsNothing`; genişleyenler:
`TestDeactivationBudget_ComesFromTheDatabase` (pencerenin iki kenarı), `TestAuditLog_IsAppendOnlyForTheApp` (00053). Üçüncü tur:
`TestInviteQueue_AClosedAPISendsNothingAndWaits`, `TestInviteQueue_AnOpenBreakerMintsNothingAndSparesThePanelsCount`,
`TestInviteQueue_AThrottlingDomainEndsUndelivered`, `TestInviteQueue_ALifetimeEndsEveryRequest`,
`TestAPIEmployees_TheListAndTheSingleReadAgree`, `TestAuditLog00054_TheDeliveryIndexIsPartialAndGoesBack`,
`TestApiKeyPanel_AMalformedBudgetIsReadAsNone`; genişleyenler: `TestInviteQueue_ACeilingDelaysAndSpendsNoTry`,
`TestKeyBudgetBanner_AbsentAddsNothing` (altın dosyalar).

**API-7** *(yazıldı — "API-7 uygulama notu"; adlar önerilenle aynı, önek eklendi)* —
`TestPunches_FourShapesTimesTwoScopes` (anlık görüntü alanlarıyla, doğru tenant'a) · `TestPunches_OutsiderSeesNone` ·
`TestPunches_NoManualNoRejected` · `TestPunches_Limit1001Is400`. Ekler: `TestPunches_TheQueryIsTheContract`,
`TestPunches_CursorGames`, `TestPunches_NothingButTheFeedsColumnsAnswers`, `TestPunches_ClosedIs403RevokedIs401SuspendedIsOpen`,
`TestPunches_ApprovalAfterTheHoldingEndedNamesNoCounterpart`, `TestPunches_PagesNeitherSkipNorRepeatUnderConcurrentWriters`,
`TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed`, `TestAPIWiring_ThePunchFeedRunsThroughTheShippedSurface`;
genişleyenler: `TestAPIWiring_TheChainTakesThePanelsPrefixAndTheOneWorker` (`Punches`), `TestOpenAPI_PathsAreTheRoutersAndTheErrorIsSection8`,
`TestStoreSurface_IsTheOneRecorded`.

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
tanığıydı; `00040` (API-1a, aşağıda) o tablodaki bütün yetkiyi `tappa_app`'ten aldığından tanık **taşındı**:
artık `pg_default_acl`'in `tappa_owner` tabloları için satırının `tappa_app`'e UPDATE ve DELETE vermesi
**ve** o satırın son yazımının göç defterinin ilk satırından **önce** olması (`age(xmin)` karşılaştırması —
zinciri sonradan elle genişletilmiş eski bir dar hacim de geçemez; `m11ArwdWitness`,
`TestM11Catalog_TheWitnessReadsTheDefaultAndItsOrder`). Tanık dar ise test CI'da kırmızıdır, başka yerde
(init dosyasından eski bir hacim) atlanır — geçiş sayılmaz — ve yeniden kurmayı söyler (ölçüldü: dar kap
yerelde SKIP, `CI=1` ile FAIL; dar zincir + sonradan genişletme `CI=1` ile FAIL; geniş kap PASS). Test, 00035'teki
ilişki/sütun/fonksiyon/dizi kümesini **dondurur** ve ondan sonra doğan her nesnenin rolü rolüne matriste
olmasını ister (`m11Tables`, `m11Columns`, `m11Functions` — `internal/db/m11schema_test.go`).
*Adlandırılmış yan bulgu — API-1a'da KAPATILDI (`00040`, 2026-10-13):* `tappa_app` göç defterinde
(`goose_db_version`) üretimin geniş varsayılanıyla UPDATE ve DELETE, **dar varsayılanla bile INSERT**
tutuyordu — ve **INSERT tek başına yeter** (API-1 güvenlik denetimi ölçtü, API-1a yeniden üretti: dar bir
kapta `tappa_app` olarak sahte bir sürüm satırı eklenince `goose up` *"no migrations to run"* dedi ve o
migration — burada `00040`'ın kendisi — hiç koşmadı; bir güvenlik düzeltmesi sessizce atlanır, defter
uygulandığını söylerdi). `00040` defterden ve dizisinden `tappa_app`'in bütün yetkisini alır ve bir son
koşulla (üyelik ya da PUBLIC üzerinden hâlâ erişiyorsa 55000) doğrular; Down defteri **kapalı tutar**
(00039'daki kodun deftere ihtiyacı yok; eski durum zaten tek bir durum değildi — `ar` ya da `arwd` — ve
hiçbiri bir karar değildi). Defteri okuyanlar goose (göç rolü), `scripts/pg-backup.sh` ve
`scripts/pg-restore-verify.sh` (ikisi `tappa_owner`) ve testlerin sahip bağlantısıdır — uygulama okumaz.
`TestMigrationLedger_AppCannotForgeAVersion`.

**`opAtVersion`.** Operatör testlerinin eski bir sürüme inmek için çalıştırdığı Down zinciri `00037` ve
`00038`'i **atlar** (`opAtVersionSkips`): ikisi operatör nesnesine dokunmaz
(`TestOpAtVersion_SkipsOnlyMigrationsWithoutAnOperatorObject`), Down'ları `employees`/`tenants` üzerinde
ACCESS EXCLUSIVE alır ve `00037`'ninki, ev lokasyonu olmayan ya da pasif/aktif aynı adresi paylaşan
satır varsa (K-14/K-24 testleri bunları commit eder) bilerek reddeder. `00039`'un Down'ı yürütülür.
API-1a'dan beri `00040` (defter) ve `00041` (holding şeması — Down'ı `tenants`'ı ACCESS EXCLUSIVE kilitler)
de atlanır; `00042` (holding'in operatör yüzeyi) yürütülür (ADR 0027 *"API-1a uygulama notu"*).


## API-2 uygulama notu (2026-10-14)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda
adıyla. Yıkıcı ölçümler ve gerçek tetikleyici mutantları atılabilir PostgreSQL 17 kaplarında
(`scripts/db-init` ile kurulu, `arwd`) koşuldu; paylaşılan `tappa-m11-db`'ye yalnız FEED ROLE bloğu
(runbook'un yolu) ve `goose up`.

**Üç migration ve bir rol.** `00048` dört tabloyu (RLS beşlisi; `punch_events` satır + TRUNCATE korumalı
append-only, `APPEND_ONLY`/`trunc_tables` listelerine dokuzuncu), `tappa_app`'in §1 matrisindeki
erişimini, her tenant'ın sayaç satırını ve yeni tenant'ın satırını açan `feed_open_counter()` +
`tenants_feed_counter` tetikleyicisini kurar. `00049` iki yazıcıyı kurar: kendi-tenant `punch_feed()`
(`tappa_feedwriter`, NOBYPASSRLS; `transactions_punch_feed` `WHEN (site_tenant_id IS NULL)` ve
`transaction_reviews_punch_feed`, ad sırasında `holding_feed`'den sonra) ve `holding_feed()`'in olay
yarısı (K-31 ön-kilidi, iki hedef olay ve teslim; `00046`'nın gövdesi `md5` ile pinli, Down onu bayt bayt
geri verir). `00050` geçmişi yazar (aşağıda, sapma 1). Rol `01-roles.sql`'in `FEED ROLE (M11 API-2)`
bloğunda, canlı küme için `deploy/README.md` → *"Feed role (M11 API-2)"*. Üçü de operatör nesnesi
adlandırmaz, `opAtVersion` atlar; Down yürüyen testlerin kilit listesine (`holdingDownTables`) dört tablo
eklendi ve `holdingBelow46` önce `feedBelow48`'i (olay satırları kenara, `00049` ve `00048` Down) yürür.

**Sapmalar — ölçümüyle:**
1. **Geri doldurma bölündü (§4'ün kendi yedeği).** `00049`'un kilidi altında tek işlemde: kazıma
   kopyalarında 700 000 satır (595 000 olay) **5,2 sn**, 2 000 000 satır **14,5 sn** — eşik 2 sn,
   önceki turların üretim vekili 688 000 satırdı. Şimdi `00049` tetikleyicileri `SHARE ROW EXCLUSIVE`
   altında kurar ve geçmiş yazmaz (700 000 satırlık kapta kilit altındaki iş 31 ms); `00050`
   (`NO TRANSACTION`) geçmişi **tenant başına küçük işlemlerle** yazar. *(Bu paragrafın ilk sürümündeki
   parça biçimi ve "700 000 satırda en uzun 0,37 sn" sayısı ikinci turda düzeltildi — aşağıda O-1: o
   ölçüm 300 tenant'a yayılmış satırlardaydı; parça başına iş tenant'ın büyüklüğüyle büyüyordu.)* **Bedel (adlandırılmış):** bir tenant'ın
   geçmişi, `00049` ile o tenant'ın parçası arasında yazılan canlı olayların **arkasından** numaralanabilir
   (imleç yine atlamaz; o sınırda `seq` sırası `occurred_at` sırası değildir); geri doldurulan olayın
   anlık görüntüsü migration anınındır (K-32 geçmiş için kurulamaz); holding satırları geri doldurulmaz
   (yalnız geliştirme/CI); teslim satırı yazılmaz. Yeniden koşulabilir (yalnız olayı olmayanı yazar).
2. **Tenants tetikleyicisi `app.tenant_id`'yi kendi kapsamında yazar.** `tappa_feedwriter` NOBYPASSRLS;
   uygulama tenant'ı kendi bağlamında ekler, ama seed, operatör araçları ve testler sahip olarak
   bağlamsız ekler ve sayaç INSERT'i `WITH CHECK`'e takılırdı (42501 — tenant'ı düşürürdü). Fonksiyon
   `SET app.tenant_id = ''` özniteliğini taşır ve gövdede `set_config(…, NEW.id, true)` yapar: ölçüldü —
   PostgreSQL, özniteliği aynı değişkeni adlandıran fonksiyondaki `SET LOCAL`'i dönüşte geri alır,
   çağıranın değeri (boş da başka bir tenant da) aynen döndü (§5'in *"gövdede set_config sızıyordu"*
   ölçümü özniteliksiz gövdeydi). Satır yalnız `NEW.id`'nindir, `last_seq` 0, `ON CONFLICT DO NOTHING`.
   `scripts/redline-check.sh`'in R5 *"düz SET"* uyarısı **boş değer** için muaf tutuldu (boş değer
   bağlantıya tenant bırakmaz — fail-closed); tenant değeri yazan düz SET yine WARN.
3. **`recorded_at` = tetikleyicinin `now()`'ı**, `transactions.created_at` değil: `created_at` çağıranın
   yazabildiği bir sütun (ADR 0027 §9'un *"son görülme"* gerekçesi); onaylı flag'de onay anı (plan §2
   madde 2: *"onay anında akışa girer"*). Bu yüzden `tappa_feedwriter`'ın §4 listesinden
   `transactions.created_at` düştü. Geri doldurmada `created_at` / `reviewed_at` (tarihî tek kaynak).
4. **Eşlik bitmişken onay (p).** İşverenin olayında karşı taraf anlık görüntüsü — sahanın lokasyon
   id/ref/ad/şirket kodu — NULL; `tag_uid` **tutuldu** (işverenin kendi satırındadır, `site_tag_uid`;
   sözleşme `tagId`'yi her satırda dolu verir — §8). Bu durumda yalnız işverenin sayacı kilitlenir (tek
   hedef; sahanın sayacına dokunmak eşi bitmiş tenant'a yeni bir bağımlılık olurdu).
5. **`USAGE ON SCHEMA public`.** Bu kümelerde (geliştirme, CI, atılabilir kap — PostgreSQL 15+'ın
   varsayılanı) PUBLIC'in `USAGE`'ı **var**; §4'ün *"bu kümede PUBLIC'in USAGE'ı yok — 4. tur"* cümlesi
   bu kümeler için geçerli değil. Rolün kendi grant'ı kemer olarak kalır; tek başına zorunluluğu PUBLIC'in
   grant'ı da alınarak ölçülür (`TestFeedwriter_EveryGrantIsIndividuallyNecessary`).
6. **Takılı yazıcı zaman aşımları açılışta bir kez okunur**, bağlantı başına değil: `execmodetypes_test`
   bir havuz kancası yazımını yalnız `pinLogParameters`'a tanıyor; aynı startup parametreleri her
   bağlantıyla gider, bir pooler hepsine aynı davranır. Unix soketinde TCP keepalive `0` okunur (ölçüldü)
   — orada kabul edilir. Sapmayan: değerler §5'in başlangıç değerleri (2 s / 30 s / 30·10·3).
7. **Rol reddi genel üyelikle genişledi** (`RoleFacts.MemberOfARole`, `roleFactsQuery`'nin altıncı
   sütunu): `tappa_feedwriter` BYPASSRLS olmadığı için `InheritsPrivilege` onu görmüyordu. Kapı bugün
   `IsProd()`'a bağlı; K-28'in `IsHardened()`'ı API-11'de genelleştirir.
8. **Mutantlar laboratuvarda.** Gerçek tetikleyicinin `CREATE OR REPLACE`'i paylaşılan veritabanında öteki
   paketlerin testlerine dokunurdu; bu yüzden §9'un elediği üç tasarım ve *"var et"*siz ön-kilit, aynı
   koşum aracıyla bir **kazıma şemasında** (`zz_api2_lab_*`, test kurar ve siler) ölçülür; laboratuvarın
   kontrolü gerçek sayaç mantığıdır ve yeşildir. Ölçümler (CI boyu): küresel dizi `CACHE 20` → 530 kaçan;
   ayrı işlemde sayaç → 84 kaçan; iki tetikleyici → 101 kilitlenme; *"var et"*siz ön-kilit → 59 kilitlenme
   (12 tur); kontrol 0/0. **Gerçek** `holding_feed`'in *"var et"* adımı (ve sayımı) çıkarılınca atılabilir
   kapta `TestFeed_MissingCounterRowsNoDeadlock` **52 kilitlenmeyle** kırmızı, geri yükleyince yeşil.
9. **Kendi-tenant GUC assert'i olay yazan yoldadır.** `ok`/onay dışındaki satır kilitsiz ve okumasız döner;
   olay yazan bir satır bağlamsız (ya da başka bağlamla) yazılırsa 55000. Ürünün her yazarı bağlamlıdır;
   sahip olarak bağlamsız `ok` satırı yazan test fikstürleri (ledger) bağlamla yazacak şekilde düzeltildi.
10. **Ön koşul — işlem başına tek sayaç kümesi** *(ilk sürümde "en çok bir olay-yazan satır" diye
    adlandırılmış bir ön koşuldu; ikinci turda mekanikleşti — aşağıda "İkinci tur", D-1).* §5'in sıra
    argümanı ve K-31'in kanonik kilidi bir işlemin olaylarının **tek** sayaç kümesini kilitlemesine dayanır:
    tap yolu işlem başına bir kayıt, onay yolu bir inceleme yazar (ölçüldü — `checkin.insert`,
    `review.Record`). Kümenin dışında olay yazan ikinci satır kilitten önce `TF001` ile reddedilir; kümenin
    içinde kalan (örn. önce `A@B`, sonra kendi `A`) serbesttir.
11. **Eklenen kısıtlar (sütun eklemez):** `punch_events_once UNIQUE (punch_id, tenant_id, version)` (hedef
    başına olay tam bir kez — bir hata çift olay değil yüksek sesli hata olur), `roles` kapalı küme,
    `{location}` ⇔ başka tenant, v1 değerleri, kanal; `webhook_deliveries` için §6'nın kapalı kümeleri ve
    `UNIQUE(endpoint_id, punch_event_seq)`; `webhook_endpoints` `UNIQUE(tenant_id)`, `https://` ve 2 048
    karakter. Tablolar `tenants`'a FK taşır — sahanın `tenants` satırına `KEY SHARE` holding dokunuşunda
    `site_punches`'ınkiyle aynı işlemde, aynı sınıfta (ADR 0027 §13 madde 18).

**Kilit beklemesi — adlandırıldı (ADR 0027 §13 madde 23).** Sayaç satırı yeni bir beklemedir; checkin
olay yazan her satırı savepoint'te yazar ve `punch_feed`'in 55P03'ünde de bir kez yeniden dener
(`TestFeedCounter_LockWaitIsWrittenOnceMore`; mutant — `punch_feed` koşulu çıkarılınca kayıt kayboldu).
Kalan: iki beklemeden uzun tutma (`TestFeedCounter_LockHeldThroughBothWaitsIsTheNamedCost`), sınırı
havuzun zaman aşımları.

**Ölçülen diğerleri.** Stres (CI): 12 yazıcı × 100, 4 tenant, ~1 080 commit, ~1 610 olay — 0 kaçan,
0 kilitlenme, yoğun; atılabilir kapta 16 × 400 üç koşu (~5 800 commit, ~8 700 olay her biri) — 0/0.
`tappa_feedwriter`'ın 49 (tablo, fiil, sütun) kalemi + `USAGE` tek tek zorunlu (5. sürümün 25/26'sının yerine); `tappa_holdingdefiner`'ın API-2 satırları
(`transactions.verdict/practice`, `tenants.api_enabled_at`, `webhook_endpoints (tenant_id, id, active)`,
sayaç S/I/U, iki INSERT) tek tek zorunlu (`TestHoldingDefiner_EveryGrantIsUsed`) — K-34 matrisinin
içinde, genişleme yok. K-33: M1/M2/M3 olay dalında kırmızı (KF'ye olay düştü). Kanarya dört yazılan
tabloda temiz.

**Geri yükleme.** `scripts/pg-restore-verify.sh` bölüm 6: `FEED_RESTORE_GAP=1000000`; her tenant'ın
sayaç satırı var ve son olayının en az aralık kadar ötesinde, değilse FAIL ve runbook adımını söyler;
`00048`'den eski döküm *"no punch feed"* deyip geçer. Runbook adımı (`UPDATE public.feed_counters SET
last_seq = last_seq + 1000000;`) A YOLU 6, B YOLU B4 ve tatbikatta (d3b), uygulama yazmadan önce.

**İkinci tur (2026-10-14 — güvenlik ONAY + 3 DÜŞÜK, üçüncü göz RED: B-1, O-1, D-1).**

*Migration'lar yerinde düzeltildi (orkestratör kararı, otonom).* `00048`–`00050` hiçbir zaman `main`'e ya da
üretime gitmedi; yalnız CI'nın taze veritabanlarında ve `tappa-m11-db`'de koştu. CLAUDE.md §3'ün
*"uygulanmış migration değiştirilmez, yenisi yazılır"* kuralı **yayınlanmış** migration içindir; burada bir
`00051` ya append-only `punch_events`'e UPDATE (yön sütununu sonradan doldurmak) ya da üretimde uzun
kilitli bir doldurma gerektirirdi. Ölçüldü: depoda migration dosyalarının değişmediğini denetleyen bir test,
kanca ya da checksum yok (goose sürüm tablosu yalnız numara tutar). `tappa-m11-db` 47'ye indirildi
(eski dosyalarla — yeni `00049`'un Down'ı eski şemada olmayan `direction` sütununu adlandırır): `00048`'in
Down'ı 12 002 olay varken **reddetti** (55000, tasarım gereği); yalnız test artığı olaylar silindi
(`punch_events` 12 002, uç nokta ve teslim 0; `tags` 9 649 satır **dokunulmadı**), sonra Down ve yeni
dosyalarla Up → sürüm 50 (geri doldurma 8 376 olay).

*B-1:* `make gen` (sqlc v1.28.0) — `internal/store/models.go` `FeedCounter`, `PunchEvent` (`Direction` dahil),
`WebhookDelivery`, `WebhookEndpoint`; `make gen` + `git diff --exit-code` temiz.

*K-40 (yön):* `punch_events.direction text NOT NULL CHECK (direction IN ('in','out'))`; iki tetikleyici
`transactions.type`'ı (onayda ana kaydınkini) yazar, geri doldurma da. `tappa_feedwriter`'a
`transactions.type` SELECT eklendi (onay kolu okur — tek başına zorunlu, ölçüldü); `tappa_holdingdefiner`
onu zaten okuyordu (izdüşüm) — K-34 matrisi genişlemedi. **Yönü olmayan satır:** ürün `ok` ve `flag`
satırına her zaman yön yazar (`tap.Decide`, M4-04); paylaşılan geliştirme veritabanında 614 onaylı yönsüz
flag vardı, hepsi test fikstürü. NOT NULL ile bu satırın olayı yazılamaz; onayı reddetmek (kayıt değil
ama yöneticinin kararı) yerine böyle bir satır **punch sayılmaz** — tetikleyiciler ve geri doldurma ona olay
yazmaz (K-4'ün inceltmesi, adlandırıldı).

*O-1 (geri doldurmanın maliyeti):* ilk sürüm her parçada tenant'ın bütün uygun satırlarını yeniden okuyor,
sıralıyor ve olay yokluğunu sorguluyordu — parça başına O(tenant), toplam O(N²) (üçüncü gözün ölçümü: tek
tenant 650 000 satırda medyan parça 982 ms, en uzun 7 118 ms, 326 parçanın 49'u 2 sn üstünde; ANALYZE'sız
80 000 satırda 62 sn ve 172 canlı 55P03). Şimdi her parça iki yarıdır: **kilitsiz pencere** — `(occurred_at,
id)` üzerinde keyset, `transactions`'ın `(tenant_id, occurred_at)` indeksiyle, en çok 1 000 punch — ve
**kilitli yazma** — o tenant'ın sayaç satırı, pencerenin hâlâ olayı olmayan punch'ları, sayaç, `COMMIT`.
Döngüden önce üç tablo `ANALYZE` edilir ve her parçanın işlemi sıralı taramayı, hash ve merge join'i kapatır
(`SET LOCAL`): `punch_events` döngünün altında büyüdüğü için istatistiği ilk parçadan sonra bayatlıyordu ve
"boş" sanılan tabloyu sıralı tarayan plan parçayı yine O(tenant) yapıyordu (ölçüldü: 50 000 satırlık tenant'ta
1,5 sn'lik pencereler; kapatınca 0,15 sn). Sıra artık `(occurred_at, id)` (eşitlikte `recorded_at` yok).
Ölçüm — atılabilir kap (Docker Desktop, macOS), tek tenant, geri doldurma sürerken o tenant'ın kendi ve
holding dokunuşları yazılırken: **300 000 satır** (255 parça): sayaç tutma medyanı **15 ms**, p99 0,93 sn,
en uzun 1,03 sn, 0,5 sn üstü 20; **650 000 satır** (553 parça): medyan **15 ms**, p99 1,16 sn, en uzun 1,34 sn,
0,5 sn üstü 29, toplam 52 sn; iki koşuda canlı yazmalarda **0 55P03, 0 kilitlenme**, akış 1..n, her uygun punch
tam bir olay. Medyan tenant büyüklüğünden bağımsızdır; kuyruk G/Ç duraklamalarıdır (aynı dağılım 300 bin ile
650 bin arasında büyümüyor). Eski sürümün 700 000 satır / 0,37 sn sayısı 300 tenant'a yayılmış satırdaydı ve
geçersizdir. *(Üçüncü turda düzeltildi: bu paragrafın "15 ms" medyanları `TestFeedBackfill_BigTenantLiveTapsLoseNothing`'in
FK'sız kazıma tablolarındandı, ve sıralı tarama/hash/merge kapatmasının planı "istatistik ne derse desin"
sabitlediği iddiası ölçümle çürüdü — aşağıda "Üçüncü tur".)*

*D-1 (tek sayaç kümesi — artık mekanik):* bir işlemin İLK olayı sayaç kümesini kilitler (kendi dokunuş
{tenant}, holding {işveren, saha}); aynı işlemin sonraki olay-yazan satırı ancak bu kümenin içinde kalırsa
yazılır, değilse **kilitten önce** `SQLSTATE TF001` ile reddedilir. İşaret işlem-yerel bir GUC'tur
(`tappa.feed_counters` = `<xid>:<tenant[,tenant]>`), geri alınan bir savepoint'le geri alınır (checkin'in tek
yeniden denemesi bu yüzden etkilenmez). Olay yazmayan satırlar (flag, red, manuel, ignored, practice)
etkilenmez; geri doldurma tetikleyiciden geçmez. Ölçüldü: inceleyenin ham şekli (kendi A + B@A, kendi B +
A@B; 12 yazıcı × 20 tur) gerçek tetikleyicilerle 240'ın 240'ı TF001, **0 kilitlenme**, hiçbir satır yazılmadı;
kontrol olmadan (atılabilir kapta gerçek fonksiyonlardan kontrol çıkarılınca) **182 kilitlenme**, laboratuvarda
177. **Sınırı (adlandırıldı; üçüncü turda düzeltildi):** GUC'u çağıran da yazabilir — işareti önceden yazan bir
işlem kontrolü atlatır. Atlatan işlem **başka tenant çiftlerinin** holding dokunuşlarını 40P01'e sürükleyebilir;
o kayıt yazılmaz (checkin 40P01'i yeniden denemez). Ulaşmak `tappa_app` SQL erişimi ister — aynı erişimle sayacı
tutup 55P03 kaybı üretmek zaten mümkündür —, ürün kodunda bu GUC'u yazan yol yoktur ve redline R5c adı
migration dışında reddeder. *(İlk hali "bedeli kendi işleminin kilitlenme riskidir" diyordu; iki mercek
ölçtü: pgbench, işareti önceden yazan X'e karşı ürün biçimli holding dokunuşu Z — Z'nin 29 işleminden 18'i
40P01; üçüncü göz 155'te 88 kurban.)*
Birden çok tenant'ta olay yazan test işlemleri savepoint'lere bölündü.

*Güvenlik D1 (geri yükleme):* `scripts/pg-backup.sh` dökümün kendi `COPY public.feed_counters` bölümünden
`feed_counter <tenant> <last_seq>` satırlarını manifest'e yazar; `pg-restore-verify.sh` bölüm 6 her sayacın
**dökümdeki değerinin** de en az `FEED_RESTORE_GAP` ötesinde olmasını ister ve manifest'te değeri olmayan sayaç
satırını reddeder; özellik dosyalı bir veritabanında `feed_counter` satırı olmayan manifest (eski
pg-backup.sh) reddedilir. Manifest satırları SQL'e girmeden `<uuid> <tamsayı>` biçimine denetlenir. İki ardışık
geri yükleme vakası (ikincisinde adım atlanmış): ilk yarı tek başına PASS veriyordu, şimdi FAIL; adımla PASS.
*(Üçüncü turda: kapı satırlara değil `feed_counters <n>` başlığına bakar — aşağıda.)*

*Redline:* R5'in boş değer muafiyeti yalnız `db/migrations/` altındaki ve satırın **tamamı**
`SET app.tenant_id = ''` olan satıra verilir; aynı satırda önce tenant değeri yazan `SET` (sonda) ve
migration dışındaki boş-değer satırı artık WARN (iki sonda ile gösterildi).

**Üçüncü tur (2026-10-14 — iki mercek ONAY + DÜŞÜK'ler; yeniden denetim yok, her değişiklik ölçüldü).**

*00050'nin planı istatistikten bağımsız değildi.* İkinci turun başlığı ("istatistik ne derse desin") ölçümle
çürüdü (üçüncü göz): `punch_events`'in istatistiği dolu hâliyle alınıp tablo boşalınca pencerenin `NOT EXISTS`'i
bir `Materialize` üstünde `Nested Loop Anti Join` oldu — her pencere tenant'ın bütün olaylarını okudu, 7 dakikayı
aşan pencere; istatistiksizken kilitli INSERT parça başına 4,4 sn ve 270 canlı 55P03. Bu turda ayrıca ölçüldü:
(1) önerilen "tam UNIQUE anahtarla ara" da yetmez — `tenant_id = $1 AND punch_id = $2` iki indeksle karşılanabilir
ve tenant'ı küçük sanan istatistik (boşken `ANALYZE` — dosyanın kendi ilk durumu) ikisini de bir satıra
fiyatlar: planlayıcı `punch_events_tenant_time_idx`'i seçti, yani tenant'ın bütün aralığını tarayan sondayı;
(2) istatistiksizken kimliklerin `transactions`'a düz JOIN'i, PL/pgSQL'in plan önbelleği beşinci parçadan sonra
genele dönene kadar tenant'ın her satırı üstünde iç içe döngü oldu — kilit altında parça başına 20–63 sn;
(3) `enable_*` kapatmaları planı `disable_cost` kadar pahalı gösterdiği için her parçanın ifadeleri JIT ile
derleniyordu — 237 ms'lik pencerenin 235 ms'i, JIT kapalı 1,7 ms. **Çözüm metindedir:** her arama skaler bir
alt sorgu ya da `OFFSET 0` ile çitlenmiş bir `LATERAL` alt sorgudur (PostgreSQL ikisini de join'e, `Materialize`'a
ya da hashed SubPlan'a çevirmez — satır başına bir indeks sondası), tek sargable yüklemi benzersiz bir kimliktir,
tenant `IS NOT DISTINCT FROM` ile karşılaştırılır (onu hiçbir indeks karşılamaz), `punch_events_once`
`punch_id` ile başlar (`00048`; tablonun `tenant_id` ile başlamayan tek indeksi) ve her parçada sıralı ve
bitmap tarama, tam Sort ve JIT kapalıdır. Onayın `EXISTS`'i (bir `OR`'un içinde hashed SubPlan'dı — tenant'ın
bütün onaylarını her pencerede okuyordu) aynı biçime geçti: satır başına `transaction_reviews_txn_key` sondası
(madde 6 çözüldü). **Ölçüm** — atılabilir kap, **gerçek tablolar**, tek tenant 650 000 satır (552 091 uygun),
553 parça: `ANALYZE`'lı (gönderilen yol) pencere medyanı 3 ms (en uzun 8 ms), sayaç tutma medyanı 64 ms
(p99 84 ms, en uzun 0,10 sn), toplam 39 sn; **istatistiksiz** 3 ms / 65 ms (en uzun 0,32 sn), 40 sn;
**bayat** (`punch_events` dolu `ANALYZE`'lı, sonra boşaltılmış) 4 ms / 76 ms (en uzun 0,10 sn), 46 sn; üçünde de
ilk ve son 50 parçanın ortalamaları aynı (tutma 64/64, 67/80, 66/65 ms). Gönderilen yolda tenant'ın 2 820 canlı
dokunuşu sürerken: 0 hata, en uzun bekleme 0,23 sn, akış 1..554 911, her uygun punch tam bir olay, yönleri
`transactions.type`. Karşılaştırma için ikinci turun dosyası aynı kapta: bayat istatistikle pencere medyanı
238 ms, istatistiksiz ~1 sn ve bir pencerede 53 sn. Kazıma tablolarındaki test (`TestFeedBackfill_BigTenantLiveTapsLoseNothing`,
300 000 satır) artık 13 ms medyan, toplam 5 sn. **Pin:** `TestFeedBackfill_ThePlanIsTheTexts` — `punch_events_once`'ın
ilk sütunu ve onunla başlayan tek indeks; bloğun metninde olay/onay `EXISTS`'i, `LATERAL` olmayan `JOIN` ve
sondalanan tablonun `tenant_id = v_tenant`'ı yok; parçanın kendi ayarlarıyla `EXPLAIN` — pencere
`transactions_tenant_occurred_idx` + `punch_events_once` + `transaction_reviews_txn_key`, kilitli yazma kimlikle;
`Materialize`, hash, sıralı tarama, bitmap, merge, `punch_events_tenant_time_idx` yok. Mutantlar: olay
`NOT EXISTS`'e dönünce, onay `EXISTS`'e dönünce, kimlikler düz `JOIN`'e dönünce kırmızı. **Kalan, adlandırıldı:**
yürüyüş `transactions`'ın `(tenant_id, occurred_at)` indeksine dayanır; tam Sort kapalıyken o tenant için
`occurred_at` sırası veren tek yol odur. Her koşu her tenant'ın uygun satırlarını kilitsiz bir kez yürür
(650 000 satırın hepsi olaylıyken 1,8 sn, soğuk önbellekle).

*Geri doldurmanın yönü pinlendi.* `00050`'de `n.type` ters çevrilince üç geri doldurma testi yeşil kalıyordu
(biri yalnız `'in'` yazıyordu). İki test artık karışık in/out yazar ve her olayın `direction`'ının punch'ının
`transactions.type`'ı olduğunu, iki değerin de geçtiğini ister. Mutantlar (ters; sabit `'in'`) iki testte de
kırmızı.

*TF001 atlatmasının bedeli düzeltildi* (yukarıda D-1'in sınırı) ve **redline R5c** eklendi: `tappa.feed_counters`
adı `db/migrations/` dışında hiçbir yerde (ürün kodu, betik, dağıtım, test) geçemez — FAIL.
`TestRedline_TheCounterSetMarkIsTheTriggersAlone`: migration'da yok, Go/test/betik/dağıtım dosyasında FAIL;
kapsam genişletme ve ad değiştirme mutantları kırmızı. Hata mesajı da kurala uyduruldu: *"this event needs a
counter outside the set this transaction's first event locked; one counter set per transaction"* (`A@B` →
kendi `A` serbesttir; testler mesaja değil koda bakar).

*Tenant'sız veritabanının geri yüklemesi.* Boş `feed_counters` manifest'e satır yazmıyordu; kapı satıra
baktığı için böyle bir manifest eski `pg-backup.sh`'ninkine benziyordu ve geri yükleme kalıcı reddediliyordu
(yeni yedek de aynı). `pg-backup.sh` artık döküm tabloyu taşıdığında, 0 dahil, `feed_counters <n>` başlığını
yazar; `pg-restore-verify.sh` bölüm 6 başlığa bakar: yoksa eski yedek (FAIL, `deploy/README.md`'nin elle yolu),
birden fazla ya da sayı değilse veya satır sayısını tutmuyorsa FAIL, 0 ve satırsızsa SQL boş bir döküm
tablosuyla koşar ve geçer. `TestRestoreVerify_FeedManifestGate` bölümü veritabanı taklit edilerek koşar
(yedi manifest); eski kapı mutantı ve sayı denetimini kaldıran mutant kırmızı. `deploy/README.md` "Eski
manifest'li yedek": tek yedek oysa sayaçları elle ileri at, iki sorguyla denetle, olay kaydına yaz.

*Migration'lar yine yerinde* (`00048`'in `punch_events_once` sütun sırası, `00049`'un hata mesajı ve yorumu,
`00050`'nin bloğu) — ikinci turun gerekçesiyle: yayınlanmamış migration, M11 dalı.

**Dördüncü tur (2026-10-14 — üçüncü göz RED: bir ORTA, iki DÜŞÜK; ürün davranışında kusur yok).**
*ORTA:* `TestFeedBackfill_ThePlanIsTheTexts` istatistiğe bağlıydı — taze, yalnız migrate edilmiş bir kapta tek
başına her seferinde kırmızı (ölçüldü, önceki sürüm): boş `transaction_reviews`'ta planlayıcı onay sondası için
`transaction_reviews_tenant_idx (tenant_id, transaction_id)`'i seçer ve kimliği **ikinci sütun** koşulu yapar
(bütün indeksin taraması). Zararsızdır ve `00050`'de adlandırıldı: indeksin boyutu istatistikten değil dosyadan
okunur, tam tarama gerçek sayfalarla fiyatlanır ve yalnız indeks küçükken seçilir (`reltuples = 0` ve büyük
tabloyla `txn_key` seçildi; en uzun tutma 0,437 sn). `00050`'nin "başka her indeks `tenant_id` ile başlar"
cümlesi `punch_events`'e daraltıldı. Test artık indeks adı istemez, planın **biçimini** yargılar: Materialize,
hash, merge, hashed SubPlan, sıralı/bitmap tarama, koşulsuz indeks taraması ve bir tenant'ın **aralığını**
tarayan (kimliksiz `tenant_id` koşullu) tarama yasak; izinli olan pencerenin `(tenant_id, occurred_at)` ile tek
yürüyüşü ve sayaç satırı (`feed_counters`'ın anahtarı `tenant_id`). Taze kapta tek başına 5/5 yeşil; VACUUM'lu
boş `transaction_reviews`'la, büyük yığınla istatistiksiz, `ANALYZE`'lı ve `reltuples = 0` iken yeşil.
*DÜŞÜK:* test ayarları ad ve değerle pinler (`enable_seqscan`, `enable_bitmapscan`, `enable_sort`, `jit` = `off`
— tam küme) ve üç `LATERAL`'ın üç `OFFSET 0` çitini sayar; dört ayarın her biri tek tek `on` ve bir çit
kaldırılınca kırmızı (üçüncü göz: `jit on` tek başına tutma medyanını 0,544 sn'ye, toplamı 25 sn'den 5 dk'ya
çıkarıyordu). *DÜŞÜK:* R5c harf ayırmaz ve ayar adının tırnaklı/boşluklu yazılışını da arar
(`tappa\s*"?\s*\.\s*"?\s*feed_counters`), kök `Makefile`'ı da tarar; pin altı yazılışı ve beş konumu
dener; eski desen, `-i`'siz, Makefile'sız ve tırnaksız mutantlar kırmızı. **Sınırı (kuralın başlığında):** parçalardan
kurulan ad (Go'da birleştirme, SQL'de `format()`) satır grep'inin göremeyeceği yoldur — kural kanıt değildir.


## API-3 uygulama notu (2026-10-15)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda
adıyla. Yıkıcı ölçümler ve mutantlar atılabilir bir PostgreSQL 17 kabında (`scripts/db-init` ile, `arwd`);
paylaşılan `tappa-m11-db`'ye yalnız `goose up` (→ 51).

**Migration `00051`.** `api_keys(tenant_id, id, name, token_hash, prefix_hint, created_by, created_at,
last_used_at, revoked_at)` — RLS beşlisi (`api_keys_tenant_created_idx` `tenant_id` ile başlar), `token_hash`
**küresel** tek sütunlu UNIQUE (çözümleyicinin en çok bir satırı oradan), `created_by` aynı tenant'ın
yöneticisine bileşik FK. `resolve_api_key_by_hash(p_token_hash text) → TABLE(tenant_id, key_id, revoked_at,
api_enabled_at)`: `tappa_resolver` sahipli, `SECURITY DEFINER`, `STABLE`, `search_path = pg_catalog, pg_temp`,
nitelikli adlar, `tenants`'a birincil anahtarla join, EXECUTE yalnız `tappa_app`'te (PUBLIC'ten alındı).
`tappa_resolver`'ın `tenants` üzerindeki erişimi **yalnız** `SELECT (id, api_enabled_at)`; ön koşul rolün
`tenants`'a önceden bir erişimi varsa düşer (sütun grant'ı onu daraltamaz), son koşul başka bir sütunu okuyamadığını
ölçer. Go tarafı `internal/db.GetAPIKeyByHash` (elle — sqlc `RETURNS TABLE`'ı tipleyemez; M1-08 notunun dördüncü
tekrarı). Down tabloyu ve iki fonksiyonu düşürür, iki sütun grant'ını geri alır — anahtarlar tabloyla gider (fail-closed:
hiçbir entegrasyon anahtarı çözülmez), izleri `audit_log`'da kalır. `opAtVersion` atlar (operatör nesnesi yok);
DROP TABLE `tenants` ve `admin_users`'ı kilitlediği için ikisi `holdingDownTables`'a eklendi.

**Sapmalar ve ADR'nin açık bıraktıkları — kararıyla (otonom, gerekçeli):**
1. **`tappa_app` `token_hash`'i okuyamaz** — §1 matrisinin çıplak *"SELECT"*inden **dar**: paneli hiçbir yol hash
   okumaz, okuyan tek yol çözümleyicidir; sütun SELECT'i yokken bağlamsız doğrudan okuma 42501 (`TestApiKeysCatalog`).
   INSERT sütun listesiyle `(tenant_id, name, token_hash, prefix_hint, created_by)` — `id`/`created_at` varsayılan.
2. **Tek yönlü iptalin kendi tetikleyici fonksiyonu** (`api_keys_revocation_is_one_way`, `00011`'in
   `tappa_forbid_revocation_reset`'i değil): §1 madde 3 M11'in tetikleyici fonksiyonunda EXECUTE'u yalnız sahipte ve
   `TG_RELID` assert'i ister; `00011`'inki PUBLIC EXECUTE'lu ve assert'siz. WHEN koşulu `00011`'inkinin aynısı.
   `00011`'in BOUNDARY 1'i burada `api_keys_revoked_after_created` CHECK'iyle kapatıldı; BOUNDARY 2 sorgularda
   (`revoked_at IS NULL` + `COALESCE`). Mutantlar: tekli iptalin bekçisi çıkınca 16 yarışçının 16'sı "kazanır" ve her biri
   iz satırı yazar; toplu iptalin bekçisi çıkınca önce tek tek iptal edilen anahtar ikinci (bulk) satırı alır — ikisi de kırmızı.
   Bekçisiz ifadenin iptal edilmiş anahtarda 23001 ile düştüğü testin içinde gösterilir.
3. **Canlı ad tekilliği** (`api_keys_live_name_key`, `(tenant_id, lower(name)) WHERE revoked_at IS NULL`): ham anahtar
   yalnız oluşturma yanıtında olduğu için form idempotent yapılamaz; yeniden gönderilen form (yenileme, geri tuşu) bu
   indekse çarpar — **422, ikinci anahtar yok** (`TestApiKeyPanel_TheKeyIsShownOnceAndNeverAgain`). İptal edilen
   anahtarın adı yeniden kullanılabilir.
4. **`prefix_hint` = önek + gövdenin ilk 4 karakteri** (ör. `tt_live_` + 4): 256 rastgele bitin 24'ü. Sahibin birkaç
   anahtarını listede ayırmaya yeter; geriye 232 bilinmeyen bit kalır, tahmin uzayını anlamlı küçültmez. Log'a yazılmaz
   (log yalnız anahtar kimliği taşır); audit `detail`'inde ad ile birlikte durur.
5. **HMAC girdisi** `"tappa/apikey/v1|"` + anahtarın **tamamı** (önek dahil) — `internal/invite`'ın alan etiketi
   kalıbı; ayrı env anahtarına ek ikinci ayrım. Bilinen-cevap vektörü Go dışında (Python `hmac`) hesaplandı
   (`TestKeyedHash_KnownAnswer`). Base64url **katı** çözülür (kanonik olmayan son karakter reddedilir).
6. **Zincirin yapı taşları** (`internal/apikey`): `ParseBearer` → `Authenticator.Check` (biçim; DB yok) → *(API-4'ün IP
   kovası)* → `Resolve` (çözümleyici + karar: bilinmeyen/iptal 401, kapalı 403; iptal kapalıdan önce gelir — iptal edilmiş
   anahtar hiç kimlik değildir) → *(API-4'ün anahtar kovası)* → `Touch` (`last_used_at`, dakikada en çok bir, iptalde yok).
   `StatusOf` hata→durum eşlemesi; veritabanı hatası 500. K-23: `Principal.Actor()` (= anahtar kimliği) ve
   `Attribution{Via: "api"}` API-4+'ün audit satırları için hazır; panelin satırları `via: "panel"`.
7. **Panel** yeni bir sekme: **API keys** (`/admin/api-keys`, `OwnerOnly`) — Billing emsali: yönetici sayfada ve üç
   yazmada **403**, reddedilen yazma `api_key.refused` iz satırı bırakır (sayfa görüntüleme yalnız log — `refuseBillingRead`
   gerekçesi). Alan da rolü kendi işleminde `admin_users`'tan yeniden okur (`mayEditBrand` emsali). **Toplu iptal**
   (*"Revoke every key"*, işaretli onay kutusu) panel eylemi olarak eklendi: sızıntıda hangi anahtar bilinmiyorsa;
   iptal ettiği her anahtar için `bulk: true` tek satır. Yazmalar `mountWriting` zincirinde (Origin kontrolü çözümleyiciden
   önce; çapraz site POST rol kapısına bile ulaşmaz — `TestApiKeyPanel_CrossSiteWritesAreRefusedBeforeTheResolver`).
8. **API erişimi kapalı tenant'ta (K-22) anahtar oluşturulmaz** — §2'nin kapı listesinde anahtar oluşturma yoktu; *"kapı
   her yolda"* ilkesiyle eklendi (alan `ErrAPIClosed`; panel bilgi notu gösterir, oluşturma formunu çizmez). **İptal kapalıyken
   de açıktır** (K-26'nın savunma yazması).
9. **Webhook imza anahtarının öneki API-3'te seçildi: `tt_whsec_`** — §6 *"önek API-9'da seçilir"* diyordu, ama R7d'nin
   ikinci değer kalıbı bu görevde isteniyor. API-9 bu öneki kullanır ya da R7d'yi aynı değişiklikte günceller.
10. **`TAPPA_API_KEY_HMAC_KEY` zorunlu, `TAPPA_WEBHOOK_KEK` API-9'a kadar isteğe bağlı** (okuyan kod yok; zorunlu yapmak
    her kurulumu kullanılmayan bir sır tutmaya zorlardı); ikisi de `namedKeys()`'te. **Ayrım kuralı genelleşti:** sekiz
    anahtarın 28 çiftinin hepsi karşılaştırılır (`keysSeparation`; `operator` bayrağı kalktı); kendi cümlesi olan iki çift
    (oturum/davet, KEK/önceki KEK) iki kez raporlanmasın diye genel kuraldan atlanır. Mutant (eski operatör-çifti kuralı):
    28 çiftin 13'ü açılır. **Bedel (adlandırıldı):** ölçülmemiş bir eşit çift artık açılışı reddeder — prod'da API-13
    runbook'u, geliştiricide yerel `.env` (ör. aynı değeri taşıyan oturum anahtarı ve etiket KEK'i artık başlamaz).
    `deploy/k8s/20-app.yaml` `TAPPA_API_KEY_HMAC_KEY`'i **isteğe bağlı olmayan** `secretKeyRef` ile alır: değer
    `tappa-secrets`'ta yokken manifest kümeye ulaşırsa kubelet pod'u başlatmaz — API-13'ün deploy öncesi adımı.
11. **Redline eklemeleri önce ağaçta sayıldı (hepsi 0 yeni isabet):** R4 şema nitelikli (`public.`, tırnaklı, boşluklu) ve
    tırnaklı `"last_ctr"`; takma ad biçimi (`UPDATE tags t SET`) **denendi, seçilmedi** — 1 yanlış pozitif
    (`marketing_claims_test.go`'nun kasıtlı mutant dizesi). *(İkinci turda düzeltildi: bu satır-yerel ek **boşa
    çalışıyordu** — gerçek sayaç yazmalarının beşi de çok satırlı ve takma adlı; R4'ün ifade kolu aşağıda, "İkinci tur".)* R7 kökleri `api_?key`, `bearer`, `signing_?key` ve log çağrısı
    içinde `Header.Get("Authorization")`/`Header["Authorization"]`; awk'ın muafiyet kararı kaçışlı başlık kalıbı yerine daha
    geniş `authorization` kelimesini okur (muafiyeti zorlaştırır). Bu yüzden API-3'ün hata ve log metinleri `apikey:` değil
    `api key:` yazar. R7b `.FirstName`/`.LastName`/`.EmployeeName` ve `first_name`/`employeeName` anahtarları. R7d yeni sınıf
    `tappa-key`: `tt_live_`/`tt_test_`/`tt_whsec_` + ≥ 43 base64url (ikinci turda ≥ 32), dolgu (en çok 4 farklı karakter) sayılmaz — belgede
    örnek böyle yazılır (`tt_live_` + 43 `x`, ya da `<…>` yer tutucusu). Her eklemenin tek başına geri alınması testi tam o
    kuralın dosyalarında kırmızıya çevirir (ölçüldü).
12. **Log ve sır:** `Token` ve `Checked` (hash) her yazdırma yolunda yer tutucu basar (`fmt` fiilleri, dışa kapalı alan,
    `slog` metin/JSON, `encoding/json`); paket log yazmaz, hataları sabit nöbetçilerdir. Panel log'u yalnız tenant, aktör ve
    anahtar kimliği taşır; ham anahtar yalnız oluşturma yanıtının sayfasında (`Cache-Control: no-store` — panelin her sayfası).

**İkinci tur (2026-10-15 — iki mercek ONAY + DÜŞÜK'ler; yeniden denetim yok, her değişiklik ölçüldü).**

*R4'ün ifade kolu.* İlk turun eki (yalnız `public.` önekli satır-yerel desen) gerçek yazmaların hiçbirini görmüyordu: beşi de
çok satırlı ve takma adlı (`tags.sql`'in `AdvanceTagCounter`'ı, `00043`'ün ve `00045`'in iki gövdesi); `UPDATE ONLY`,
takma adlı ve bölünmüş sondaların üçü de geçiyordu. Şimdi ikinci bir kol `rg -U` ile `UPDATE [ONLY] [public.]tags [[AS] t] SET
last_ctr`'dan ilk `;`'e ya da backtick'e kadar **ifadenin tamamını** okur ve her ifadede `<ad>ctr <` biçimli **katı** bir
karşılaştırma arar (`<=` sayılmaz) — koruma CTE takma adında olsa da (`prev.old_ctr < @ctr`, `v.old_ctr < p_ctr`) görülür.
Ölçüldü: ağaçta 0 FAIL; beş gerçek ifadenin korumasının silindiği kopyada dördü (`tags.sql:39`, `00043:257`, `00045:75` ve
`:131`) FAIL. `_test.go` ifade kolunun **adlandırılmış istisnasıdır**: testler kasıtlı korumasız yazmalar taşır
(`tagsinventory_test.go`'nun üç tetikleyici sondası, `marketing_claims_test.go`'nun `<=` mutant dizesi) — dahil etmek dört
satırı FAIL yapardı; testler satır koluyla taranmaya devam eder (o kol `ONLY`'yi de öğrendi). **Kalan sınırlar (kanıt
değildir):** Go'da parçalanmış dize (`"UPDATE tags " + "SET …"`), anlamsız bir karşılaştırma (`ctr < 999999`), PL/pgSQL
gövdesinin başka bir parçasındaki `ctr <`, dinamik SQL. §4.4'ün gerçek garantisi değişmedi: katı yüklem, `00013`'ün
`tags_counter_monotonic` tetikleyicisi ve yarış testleri.

*R7d.* `tappa-key` sınıfı solundaki kelime karakterinde eşleşmeyi atlıyordu; iki istisna eklendi — `%XX` (URL kodlaması:
`Bearer%20tt_live_…`, `%3Dtt_…`) ve `\n`/`\r`/`\t` (JSON ya da Go dizesi kaçışı). Gövde eşiği 43'ten **32**'ye indi (42
karakterlik kırpılmış anahtar 4 bilinmeyen bit bırakır — sızıntıdır); ağaçta 0 isabet (sayıldı). İki satıra bölünmüş anahtar
`secretscan.sh` başlığında **kanıt değildir** sınırı olarak adlandırıldı.

*R7.* Değerin kendisini taşıyan iki ad — `RevealToTheOwnerOnce()` ve panelin `NewKey`'i — ve `Header.Values("Authorization")`
eklendi (log çağrısında 0 isabet, sayıldı). Her yeni parçanın tek başına geri alınması
`TestRedline_R4R7R7bR7dCatchDeliberateViolations`'ı tam o kuralın dosyalarında kırmızıya çevirir (ölçüldü: ifade kolu kapalı → 3
dosya; `ONLY` düşünce → 1; koruma `<=`'yi kabul edince → 1; testler dahil edilince → kontrol FAIL; değer adları → 2;
`Header.Values` → 1; kaçış istisnası → 5; eşik 43 → 2).

*`IntegrationKeys` bağlantısı (D3).* `NewAdminAuth` `TAPPA_API_KEY_HMAC_KEY`'i taşıyan (yani her üretim) yapılandırmayı anahtar
servisi verilmeden **reddeder** (`TestNewAdminAuth_TheKeysServiceMatchesTheConfiguration`); `main.go`'nun bağlantısı AST ile
pinlendi (`TestAPIKeyWiring_TheKeysServiceReachesThePanel`: tek `apikey.NewKeys(apikey.LivePrefix, cfg.APIKeyHMACKey, …)`,
sonucu `handler.IntegrationKeys` ile `NewAdminAuth`'un yaydığı seçeneklere, ikisi de `run()`'ın kendi gövdesinde).
Mutantlar (ekleme silindi, bir `if` içine taşındı, önek `SandboxPrefix`, ret kaldırıldı) kırmızı.

*Toplu iptal yarışı (D2).* `now()` işlemin başlangıcıdır: toplu iptal başladıktan sonra oluşturulup commit edilen bir anahtara
`revoked_at < created_at` yazılıyor, `api_keys_revoked_after_created` (23514) bütün iptali geri alıyordu (iki psql oturumuyla
ölçüldü). İki iptal de artık `COALESCE(revoked_at, clock_timestamp())`
(`TestApiKeyRevocation_AKeyCreatedDuringRevokeAllIsRevokedToo`, kancayla kurulan yarış; `now()` mutantı 23514 ile kırmızı).

*Test boşlukları (D4).* `bulk` bayrağı geri okunuyor (tekli iptalde `false`, taze anahtarlar üzerinde toplu iptalde her satır
`true`; `bulk=false` mutantı kırmızı). `TestApiKeyAuth_NothingLogged` artık `%v %+v %#v %s %q %x %X %d` fiillerini, tek başına,
dilim ve harita içinde, ve `encoding/gob`'u dener; hex ve tırnaklı kanaryalar eklendi. Mutantlar kırmızı: yalnız `%q`'da sızan
`Format`, yalnız `%x`'te hex basan, `%s`'te hash'i basan `Checked`, değeri döndüren bir `MarshalBinary`.

*Panel.* Oluşturma başarılı olup ardından liste okuması düşerse anahtar yine gösterilir (200, tek cümleyle; liste, form ve kapı
notu çizilmez) — `TestApiKeyPanel_TheKeyIsShownEvenWhenTheListCannotBeRead`; ilk turun biçimi (500 sorun sayfası) kırmızı.

*Devir.* `tt_test_`'in (ve panelin ve zincirin aynı önekle kurulmasının) seçimi API-4/API-11'e devredildi; plan §5'in API-4 ve
API-11 kabullerine yazıldı.


## API-4 uygulama notu (2026-10-15)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda adıyla.
Yıkıcı ölçümler ve migration mutantları atılabilir bir PostgreSQL 17 kabında (`scripts/db-init` ile, `arwd`);
paylaşılan `tappa-m11-db`'ye yalnız `goose up` (→ 52).

**Migration `00052`.** `api_idempotency(tenant_id, key_id, idem_key, request_hash, status, result_ref, created_at)` —
§8'in `UNIQUE(tenant_id, key_id, idem_key)`'i birincil anahtar (önce `tenant_id`: RLS beşlisinin indeksi), anahtar
tenant'ın **kendi** `api_keys` satırına bileşik FK (başka tenant'ın anahtar kimliği 23503), `idem_key` 1–255 görünür
ASCII, `request_hash` 64 hex, `status` NULL ya da 200–299. `tappa_app`: REVOKE ALL; SELECT bütün sütunlar; INSERT
`(tenant_id, key_id, idem_key, request_hash)`; **UPDATE yalnız `(status, result_ref)`**; DELETE (tembel silme); son
koşul bloğu başka sütunda UPDATE'i ve TRUNCATE/REFERENCES/TRIGGER'ı reddeder (ölçüldü: REVOKE ALL çıkarılınca `arwd`
altında migration **kendi** son koşulunda düşer; son koşul da çıkarılınca katalog testi kırmızı). Fonksiyon yok.
`opAtVersion` atlar; `api_idempotency` `holdingDownTables`'a girdi ve `00051`'in Down testi önce `00052`'nin Down'ını yürür.

**Sapmalar ve ADR'nin açık bıraktıkları — kararıyla (otonom, gerekçeli):**
1. **Idempotency'nin mekaniği** (`internal/apikey`, `Writes.Run`): tek işlem, anahtarın tenant bağlamı — (a) o tenant'ın
   24 saatten eski en çok 100 satırı (`FOR UPDATE SKIP LOCKED`; *ikinci turda kendi kısa işlemine taşındı — yazma
   işleminin içindeyken iki yazma birbirinin süpürdüğü satırda kilitleniyordu, aşağıda "İkinci tur"*), (b) aynı anahtarın süresi dolmuş satırı, (c) **iş'ten önce** `INSERT … ON CONFLICT DO NOTHING` ile talep
   (eşzamanlı ikinci istek birincil anahtarda bekler — beklemenin kendisi eşzamanlılık denetimidir), (d) iş, (e) sonuç
   (`status`, `result_ref`). `status` yalnız talep eden işlemin içinde NULL'dır; NULL'lı bir commit görülürse kesinti
   sayılır (`ErrUnrecorded`, başarı değil). Hata alan yazma hiçbir şey kaydetmez (yeniden deneme işi yeniden koşar).
   Replay aynı durum + kayıt kimliğiyle ve `Idempotent-Replayed: true` başlığıyla döner (gövde saklanmadığı için
   gövdeyi kimlikten uç kurar — API-5/6). **Ölçüm:** 16 eşzamanlı istek, ilk talep 300 ms açık tutulurken → 1 iş,
   15 replay, 1 audit satırı; okuma-sonra-yazma mutantı → 16 iş. Süreç ölümü (`pg_terminate_backend`) ve istemci
   iptali → defterde 0 satır, sonraki istek ilk istek; ADR'nin elediği seçenek (talep ayrı işlemde) mutantı kırmızı.
2. **`request_hash`** = SHA-256(`"tappa/v1-request|"` + yöntem + kaçışlı yol + sorgu + gövde, `0` baytlarıyla ayrık);
   anahtar aynı + bunlardan **herhangi biri** farklı → 422. Ad alanı `(tenant, api anahtarı)` — §8'in UNIQUE'i gereği
   bir tenant'ın iki anahtarı aynı Idempotency-Key'i paylaşmaz. Başlık tek değer, 1–255 görünür ASCII; değilse 400.
3. **Hata kodu kümesi kapalı** (`internal/handler/api/errors.go`, OpenAPI `Error.code` enum'u ile iki yönlü pinli):
   `invalid_request`, `body_too_large` (400) · `unauthorized` (401) · `api_disabled`, `tenant_suspended` (403) ·
   `not_found` (404) · `idempotency_key_reused` (422) · `rate_limited` (429) · `unavailable` (503). §8'in kümesinde 405,
   413, 415 ve 500 olmadığı için: desteklenmeyen yöntem **404 `not_found`**, büyük gövde **400 `body_too_large`**,
   kodlanmış gövde **400 `invalid_request`**, bizdeki her hata (veritabanı dahil) **503 `unavailable`**, nedeni yalnız
   log'da. Bir panik ve kaçırılmış bir süre sınırı da *(ikinci turdan beri)* 503 `unavailable` JSON'dur. 409'un kodu
   henüz yok — API-6 `email_unavailable` ve `already_activated`'ı iki tarafa birden ekler. Bilinmeyen ve iptal edilmiş
   anahtar **aynı** 401 gövdesini alır (hangi anahtarın var olduğuna kehanet yok; mutantı kırmızı).
4. **Kovalar** bellekte (`httpx.Limiter`): adres 1 200/dk (biçimden sonra, çözümleyiciden önce; ayrıca anahtarsız
   `openapi.yaml`), anahtar 600/dk (çözümleyiciden sonra). `Retry-After` pencerenin tamamıdır (Limiter kalan süreyi
   vermiyor; bugünkü tap kovasıyla aynı). **Ölçüm:** tek adresten 20 biçimli bilinmeyen anahtar, kova 3 → 3 çözümleyici
   çağrısı, 17 × 429; kova çözümleyiciden sonra konunca 20 çağrı. Kısılan istek `Touch` edilmez.
5. **`TAPPA_API_HOST` isteğe bağlı:** yoksa yüzey kurulmaz, `/v1` her host'ta 404, açılış satırı
   `integration_api=off`. Varsa tek yazım (operatör host'unun kuralı), taban URL'nin ve operatör host'unun host'u olamaz.
   `deploy/k8s/20-app.yaml` onu `tappa-secrets`'tan **`optional: true`** alır ve Secret'ta yoktur — API, API-13'ün
   adımına (ingress kuralı, K-22, runbook) kadar kapalı kalır.
6. **İki yönlü host kapısı** operatörünkinin genellemesidir: `httpx.apiHostOnly` API host'unda `/v1` dışını, yüzeyin
   `hostGate`'i başka her host'ta `/v1`'i router'ın **kendi** 404'üyle yanıtlar (ana host API'nin varlığını bile
   söylemez). Sınıflama `OnHost` (port, büyük harf, tek sondaki nokta) ve `routingPath`; `X-Forwarded-Host` **hiç**
   okunmaz (real-ip'in ilkesi: hiçbir yönlendirme başlığı burada bir şey adlandırmaz). Mutantlar (iki yarının her biri)
   kırmızı.
7. **Zincirden sonra `Authorization` başlığı istekten silinir**; bir `X-Request-Id` `tt_live_`/`tt_test_`/`tt_whsec_`
   öneki taşıyorsa (harf ayırmadan) değiştirilir — **önek taşıyan** bir değer erişim log'una istek kimliği olarak giremez
   *(ikinci turda daraltıldı: öneksiz 43 karakterlik gövde sıradan bir kimlik gibi taşınır — ölçüldü)*.
   `Touch` hatası 503'tür (sessiz geçilmez).
8. **Askı kontrol noktası** (`internal/handler/api/suspension.go`): her `/v1` yazması `Surface.write`'tan geçer; işin
   `Gate.Admit`'ten geçmediği yazma **commit edilmez** (mutantı kırmızı). Askıdayken yalnız `WriteEmployeeCreate` ve
   `WriteResendActivation` açıktır; sıfır sınıf (adlandırılmamış yazma) kapalıdır. Durum okuyucusu bu dalda
   `NoSuspensionColumn`'dır (OP-15'in `tenants.suspended_at`'i dalda yok); §4.7'nin hücre hücre tablo testi API-4b'dir.
   Panel hücreleri (anahtar oluşturma, uç nokta işlemleri, ping, "yeniden gönder"; K-26'nın iki savunma yazması açık)
   OP-15'in panel kapısındadır — eşleme dosyanın başında.
9. **K-19 işçisi** (`internal/webhook`): süreç içi küme + tek döngü; iş (`Job.RunFor(ctx, tenant)`) API-6/API-10'la
   gelir. Kümeye girenler bugün: kaydedilmiş her dokunuş (holding'de **iki** tenant — `checkin.Service.NoticeWorkTo`) ve
   kimliği doğrulanmış her `/v1` isteği. **Devredildi:** onay ve panelin "yeniden gönder"i (teslimle birlikte —
   API-9/API-10). Küme **süreç başınadır**: iki işçi aynı tenant'ı kendi işine verir (ölçüldü) — "iki pod tek gönderim"
   garantisi işçiden değil işin kirasından gelir (API-10'un kabulü). Kapanış: `drainers{resetFlow, work}`, HTTP
   `Shutdown` ile eşzamanlı, aynı bütçede.
10. **Idempotency çalıştırıcısı `internal/apikey`'de** (`writes.go`): handler paketi `store`'u import etmez (CLAUDE.md §3);
    anahtarın kimliğiyle anahtarlanan defter anahtar paketinin yanında durur. `internal/handler/api` paneli import etmez,
    panel onu import etmez (`go list -deps` ile iki yönlü ölçüldü); paket çerez okumaz, koymaz.
11. **Üretimde boş uç yok:** zinciri ve yazmayı sınayan uçlar yalnız testte (`export_test.go`'nun `withTestRoutes`'u)
    bağlanır; üretimdeki tek uç `GET /v1/openapi.yaml`'dır (anahtarsız, adres kovalı).

**İkinci tur (2026-10-15 — iki mercek ONAY + DÜŞÜK'ler; yeniden denetim yok, her değişiklik ölçüldü).**

*Tembel silmede kilitlenme (üçüncü göz D1).* Süpürme yazma işleminin içinde ve anahtarın kendi silmesinden önceydi: A
tenant'ın en eski 100 süresi dolmuş satırını kilitliyor, B onları atlayıp sonraki 100'ü kilitliyordu; A'nın anahtar satırı
B'nin kümesinde, B'ninki A'nınkinde iken iki anahtar silmesi birbirini bekliyordu — **40P01**, deterministik. Döngü
olmasa da süpürülen satır yazmanın ömrü boyunca tutulduğu için başka anahtarlı bir yazma onu bekliyordu. Şimdi süpürme
**kendi kısa işleminde** commit eder, yazma işleminin ilk ifadesi anahtarın kendi süresi dolmuş satırını siler: yazma
işleminin bekleyebileceği tek şey **aynı anahtarlı** istektir. Ölçüm (`TestIdempotency_TwoKeysOverAFullSweepNeitherDeadlockNorWait`):
251 süresi dolmuş satır, A'nın ve B'nin anahtar satırı birbirinin süpürmesinde, önce A süpürür, ikisi birlikte salınır,
A işlemini 1 sn açık tutar → hata yok, B ~14 ms'de döner; 150 eski satırın arkasındaki anahtar satırı da silinir.
Mutantlar: süpürme yazma işlemine geri → 40P01; anahtar başına silme kaldırılınca → 422. Bedel (adlandırıldı): süpürme
ile yazma arasında süreç ölürse süpürme yine commit edilmiştir — süresi dolmuş satırlar içindir, kayıp yok.

*Test edilmemiş dallar (D2).* `Touch` hatası 503 (`TestChain_ATouchThatFailsIs503`; yok sayan mutant 200). Askı okuması
hata verirse kontrol noktası **kapalıdır** — davranış zaten buydu, pinlenmemişti: her sınıf, askıda açık iki sınıf dahil,
503 ve commit yok (`TestWrite_ACheckpointThatCannotReadIsClosed`; hatayı "askıda değil" sayan mutant 201). İlk turun
*"commit anlık görüntüsünde askıda değildi"* cümlesi READ COMMITTED altında fazlaydı: `Admit` askıyı **okuduğu anda**
görür; okuma ile commit arasında commit edilen bir askı o yazma tarafından görülmez. Okumanın `tenants` satırını kilitlemesi
gerekip gerekmediğini (`FOR SHARE`, operatörün `FOR NO KEY UPDATE`'ine karşı) API-4b ölçer (plan §5).

*Askı okuyucusunun teli (iki mercek).* `TestAPIWiring_TheSuspensionReaderFollowsTheColumn`: `tenants.suspended_at` canlı
katalogda **ya da** bir migration dosyasında varken kablolama hâlâ `NoSuspensionColumn` ise kırmızı. Mutantlar: sütun
atılabilir kapta eklenince ve sütunu adlandıran bir migration dosyası konunca kırmızı. API-4b bu tel kırmızıya döndükten
sonra kapanır.

*Panik ve süre sınırı (iki mercek).* `/v1` grubunun `guard`'ı (gövde sınırından sonra): panik yığınıyla loglanır (isteğin
bağlamı; `Authorization` zincirde zaten silinmiş — kanaryayla doğrulandı) ve 503 `unavailable` JSON döner; bağlamı biten ve
hiçbir şey yazmamış istek, router'ın `middleware.Timeout`'u gövdesiz 504'ünü yazmadan önce 503 `unavailable` alır. Yazılmış
durum korunur, `http.ErrAbortHandler` yeniden fırlatılır (`TestGuard_PanicAndDeadlineAreSection8`; guard yokken 500,
süre kolu yokken 504).

*Request-id iddiası (güvenlik D1).* Mekanizma yalnız **önektir**: öneksiz 43 karakterlik anahtar gövdesi `X-Request-Id`
olarak log'a yazılır (ölçüldü; testin CONTROL satırı). Cümleler buna daraltıldı.

*Sözleşme notları.* (1) `request_hash` **bayt düzeyindedir**: sorgu parametrelerinin sırası ya da JSON'daki boşluk farkı
aynı anahtarla **422** verir; başlık farkı (ör. `Content-Type`, `User-Agent`) hash'e girmez ve **replay** verir — API-12
rehberinde entegratöre bildirilir (plan §5 API-12). (2) Anahtarsız bir istek rotanın varlığını ayırt edebilir: kayıtsız
yol 404, kayıtlı (kimlikli gruptaki) yol 401. `openapi.yaml` herkese açık olduğu için bu yeni bir bilgi vermez.
(3) `TAPPA_API_HOST` yokken `api.*` adı paneldeki her şeyi yakalayan rotalara düşer; çift noktalı host
(`api.taptime.mt..`) API host'u sayılmaz, panele gider — API-13'ün runbook adımı (plan §5 API-13).


## API-5 uygulama notu (2026-10-15)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda adıyla.
Migration yok (`locations`'ın `external_ref`/`active` sütunları ve `tappa_app`'in tablo düzeyi yetkileri API-1'den).
Mutantlar atılabilir bir PostgreSQL 17 kabında (`scripts/db-init` ile, `arwd`, goose 52); paylaşılan `tappa-m11-db`'ye
yalnız testlerin yazdığı satırlar.

**Uç.** `PUT /v1/locations/{ref}` (`internal/handler/api/locations.go`): ref → gövde → yazma (çalıştırıcının işlemi; önce
`Gate.Admit(WriteLocationPut)`, sonra alan). İş, panelin kendi lokasyon alanının yeni yöntemidir —
`tenant.Venues.PutVenueTx` (`internal/domain/tenant/venueapi.go`): ad `venueName`'den (panelin kuralı: NUL atılır, kırpılır,
1..80 rün, içte kontrol/biçim karakteri yok; kesilmez, reddedilir — *ikinci turda düzeltildi: ilk cümle "geçersiz UTF-8
atılır" diyordu; JSON yolunda ölçümle yanlıştı, `encoding/json` her bozuk baytı U+FFFD yapıp saklatıyordu; artık UTF-8
olmayan gövde 400*), ref 00037'nin kalıbından **kırpılmadan** (yol parçası; ikinci turdan beri bir kez yüzde-çözülmüş). Yeni ref
→ **201**; var olan ref, ad ya da etiket değişince → **200** + `location.updated`; aynısı → **200**, yazma ve iz satırı yok.
Yanıt `{id: loc_<uuid>, externalRef, name, timezone, active, createdAt}`; `timezone` işletmeninkidir (K-7). Üretim
kablolaması `run()`'ın panel `*tenant.Venues`'ini (panelin audit kaydedicisiyle) `openAPISurface`'e verir; AST ile pinli.

**Sapmalar ve ADR'nin açık bıraktıkları — kararıyla (otonom, gerekçeli):**
1. **Kanıt yazılmaz** (§9, plan §2 madde 10): sorgular yalnız `name`, `external_ref`, `active` adlandırır; API'nin açtığı
   lokasyon `static_ips = '{}'`, GPS/vardiya/ağ adı NULL doğar. Bu yüzden CLAUDE.md §5'in netx genişlik kuralının bu yolda
   uygulanacağı bir adres aralığı **yoktur**. Gerçek tap yoluyla ölçüldü: böyle bir lokasyona NFC dokunuşu **yazılır**,
   `flag`, güven 20 (`base:no-evidence-review`); panel aralık verince sonraki dokunuş `ok` (mutant: oluşturma bir aralık
   yazınca ilk dokunuş `ok` — kırmızı).
2. **K-20 ölçüldü:** `active:true`/`false` × kanıtsız/aralıklı dört dokunuş: kararlar ve güven çift çift aynı (flag/20,
   ok/70). Mutant (`GetLocationForTap`'e `AND active`) kırmızı. Panelin `SaveVenue`'i API'nin etiketini sıfırlamaz.
3. **Eşzamanlılık:** arama `FOR NO KEY UPDATE`, yeni ref `INSERT … SELECT FROM tenants … ON CONFLICT (tenant_id,
   external_ref) DO NOTHING`; satır dönmezse (eşzamanlı PUT commit etti) satır kilitlenip güncelleme olarak sürer. Serbest
   bir PUT seli INSERT'te nadiren buluşuyordu (yeniden denemesiz mutant 3 koşunun 2'sinde yeşil), bu yüzden test buluşmayı
   **zorlar**: başka bir bağlantıda commit edilmemiş bir satır tutulur, dört PUT'un hepsi `pg_locks`'ta onun
   `transactionid`'ini beklerken commit (→ 4 × 200, satır 1, iz 0) ya da rollback (→ 1 × 201 + 3 × 200, satır 1, bir
   `location.created`). Mutantlar: yeniden deneme kaldırılınca 503 (3/3); düz INSERT → benzersizlik kısıtı ikinci satırı
   reddeder, 503. *Ölçülmeyen:* kısıt da düşürülünce satır sayısı — `DROP CONSTRAINT` ortamın yıkıcı-yol kancasınca
   atılabilir kapta da engellendi, aşılmadı.
4. **Gövde sözleşmesi** (ADR bilinmeyen alan politikası koymuyor): `name` ve `active` zorunlu; `timezone` isteğe bağlı,
   varsa işletmeninkiyle **bayt bayt** eşit (`europe/malta`, `UTC`, sonda boşluk → 422); `externalRef` isteğe bağlı, varsa
   yoldakine eşit. **Başka her anahtar 422** ve anahtarlar **tam** eşleşir (`encoding/json`'un harf katlaması yok):
   yanlış yazılmış `"acitve":false` sessizce düşseydi etiket değişmeden 200 dönerdi; bir koordinat ya da adres aralığı
   kabul edilmiş görünmemeli (API kanıt yazmaz). Ret bilinmeyen anahtarı yankılamaz (`errors.go`'nun kuralı), izinli
   listeyi verir. Mutant (harf katlamalı eşleme) kırmızı. Bozuk JSON / nesne değil / iki değer → 400 `invalid_request`.
5. **İki yeni hata kodu** (`errors.go` ve OpenAPI `Error.code` enum'unda, iki yönlü pin): `validation_failed` (422;
   `details.field` + kapalı `details.reason`: `required`, `type`, `invalid`, `mismatch`, `unknown_field`) ve
   `timezone_mismatch` (422; `details.businessTimezone` — isteğin değil işletmenin kendi ayarı; kodun "details yalnız
   sabit" kuralının adlandırılmış tek istisnası). Ref biçimi 422 (`field: ref`) — API-1 notunun "API-5/6 bu biçimleri
   422 ile söyler"i.
6. **K-7'nin okuması kilitsiz:** saat dilimi `GetTenantClock` ile işlemin içinde okunur; lokasyonun kendi dilimi
   olmadığı için eşzamanlı bir dilim değişikliği hiçbir satırı eski dilimde bırakamaz.
7. **Askı:** `WriteLocationPut` kapalı sınıf; yeni ref, var olan ref ve **değişmeyen** PUT da 403 `tenant_suspended`
   (kontrol noktası alandan önce); hiçbiri lokasyon, iz ya da Idempotency-Key satırı bırakmaz. Bu dalda askı okuyucusu
   `NoSuspensionColumn` olduğundan test kendi `Suspension`'ını verir; hücre tablosu yine API-4b'dir. Mutant (açık sınıf)
   kırmızı.
8. **Idempotency yanıtı:** defter yalnız durum + lokasyon kimliği tutar (§8); replay lokasyonu **şimdiki hâliyle**
   döner (araya giren bir yeniden adlandırma replay'de görünür — ölçüldü). Defterdeki lokasyon panelden silinmişse
   replay 404 `not_found`.
9. **Audit (K-23):** `location.created` / `location.updated` (panelin eylem adları), `actor_id` = anahtar kimliği,
   `detail` = `{via: "api", external_ref, [from_name], to_name, [from_active], to_active, lost_proof_of_place}` —
   amaca özel struct, anahtar/başlık/gövde yok (test anahtarı ve `bearer`'ı arar).
10. **Kapsam ağı (§4.5 kemeri):** altı `query_test.go` kopyasının `whereClauses`'ı `ON CONFLICT`'te durmuyordu —
    `CreateLocationFromAPI`'nin WHERE'i "`t.id = @tenant_id ON CONFLICT …`" okunup doğru kapsamlı ifade bayraklanıyordu
    (ölçüldü). Belirteç altı kopyaya da eklendi (kopyalar anlaşsın). Mutant: aramadan `tenant_id` yüklemi atılınca kemer
    kırmızı; HTTP izolasyon testleri RLS sayesinde yeşil kalır (kuşak + kemer).

**Uçtan uca (görevin ölçüsü).** `TestAPIWiring_ThePutOfALocationRunsThroughTheShippedSurface` yüzeyi komutun kurduğu gibi
(`openAPISurface`, gerçek havuz, panelin anahtar servisi ve lokasyon alanı) kurar: panelin verdiği anahtar API host'unda
kabul, ana host'ta 404; Idempotency-Key ile tek defter satırı (201, `result_ref` = lokasyon) lokasyon ve iz satırıyla aynı
işlemde; tekrar replay; iz satırı anahtarın; `last_used_at` yazıldı; K-19 tenant'ı gördü. Mutant: iş `Gate.Admit`'i atlayınca
503, hiçbir şey commit edilmez.

**İkinci tur (2026-10-15 — güvenlik ONAY + 3 DÜŞÜK, üçüncü göz RED: bir ORTA test açığı, kodda hata yok).**

*ORTA — yalnız etiket değişimi testsizdi.* "Değişmedi" kısa devresi yalnız adı karşılaştıracak biçimde bozulunca
(`before.Name == name`) iki paket yeşil kalıyordu: aynı ad + `active:false` 200 dönüp etiketi `true` bırakıyor, iz satırı
yazmıyordu. `TestLocationsPut_CreatesUpdatesAndAnswersUnchanged` artık aynı adla `false`, sonra yeniden `true` gönderir: her
biri 200, satır izler, her biri `from_active`/`to_active`'li bir `location.updated`. Mutant kırmızı.

*Lokasyon adında kontrol ve biçim karakteri (güvenlik D1).* BEL, ESC, satır sonu, U+202E ve sıfır genişlikli karakterler
201 alıyordu; ad holding eşine de anlık görüntüyle gider. `venueName` (lokasyon ve departman, panel ve API) kırpmadan sonra
Cc/Cf karakterini reddeder; panelin sınırı aynı yüklemi (`tenant.NameHasHiddenCharacters`) cümleyle söyler (`parseName`).
Ölçüldü (**paylaşılan geliştirme veritabanı** `tappa-m11-db`, üretim değil): böyle bir ad taşıyan 23 lokasyon satırının
hepsi bir CSV testinin ham INSERT'i (`reportscsv_db_test.go`, bilerek `venueName`'i atlar); departmanda 0. *(Üçüncü turda
kural daraltıldı ve kayıtlı ad muaf tutuldu — aşağıda.)* NUL eskisi gibi önce atılır. Mutantlar (alan ve panel
yarısı ayrı ayrı) kırmızı.

*Kemer, INSERT … SELECT ve DO UPDATE (güvenlik D2, D3).* `internal/domain/tenant/query_test.go`: (i) INSERT … SELECT'in
seçim listesinde `tenant_id` konumundaki değer `@tenant_id` ya da kapsamlı kaynak satırın kendi kapsam sütunu (`t.id`,
`l.tenant_id`) olmalı — `SELECT @other_tenant::uuid, … FROM tenants t WHERE t.id = @tenant_id` artık bulgu; (ii) ON
CONFLICT … DO UPDATE bloktan ayrılır ve WHERE'i INSERT'in **hedefine** karşı denetlenir — önce `DO UPDATE SET x` "set"
adlı bir tablonun UPDATE'i okunuyordu (yanlış alarm). `TestInsertSelectAndDoUpdate_TheBeltReadsThem` (beş kırmızı, beş yeşil
biçim); iki kolun her biri kaldırılınca kırmızı. **Sınır:** öğe metin olarak karşılaştırılır (döküm ve `AS` atılarak); aynı
değeri *hesaplayan* bir ifade (CASE, COALESCE) reddedilir, anlaşılmaz; değerin son kemeri RLS `WITH CHECK`. *(Bu
paragrafın ilk hâli "öteki beş kopyada INSERT kolu hiç yoktur, paketleri okur ya da düz yazar" diyordu — yanlıştı:
adminauth, signup ve review `INSERT … SELECT` çağırır; üçüncü turda düzeltildi, aşağıda.)*

*Yüzde kodlanmış ref (üçüncü göz).* `%41BC` ve `%7E` (Java `URLEncoder`'ı `~`'yi kodlar) 422 alıyordu. Seçim: **bir kez
çöz, sonra aynı katı kalıp** — rehbere "kodlamayın" yazmak her istemci kütüphanesini denetlemeyi entegratöre bırakırdı.
chi'nin `{ref}`'i kararlı bir kaynak değil (net/url `RawPath` tutunca kodlu, tutmayınca çözülmüş parça — `%2541` ona
`%41` olarak gelir, yeniden çözmek iki kez çözmektir); bu yüzden parça isteğin **kodlu** yolundan okunup bir kez çözülür.
`/`, kontrol karakteri ve çift kodlama (`%2541BC`) yine 422. Aynı ref'in iki yazımı tek lokasyondur; Idempotency-Key için
iki ayrı istektir (istek hash'i kodlu yolu okur). Mutantlar: çözme kaldırılınca `%41…` 422; çözülmüş `Path`'ten okuma
(iki kez çözme) `%2541BC`'yi 201 `ABC` yapar — ikisi de kırmızı.

*Yinelenen JSON anahtarı (üçüncü göz).* `{"active":true,"active":false}` sessizce `false` oluyordu; nesne artık üye üye
okunur, kaçışlı yazım dahil (`"n\u0061me"`) iki kez geçen anahtar **422 `validation_failed`, `reason: duplicate_field`**.
Mutant kırmızı.

*Geçersiz UTF-8 (üçüncü göz).* UTF-8 olmayan gövde JSON değildir (RFC 8259 §8.1) — **400 `invalid_request`, `reason:
invalid_utf8`**; U+FFFD'yi ad içinde reddetmek yerine bu seçildi: kaçışla gönderilmiş gerçek bir U+FFFD istemcinin kendi
seçimidir, bozuk bayt değildir. Mutant (kontrol kaldırılınca U+FFFD'li ad 201) kırmızı.

*Rehber notları* plan §5 API-12'ye yazıldı (bilinmeyen alan, yinelenen alan, UTF-8, ref kodlaması).

**Üçüncü tur (2026-10-15 — üçüncü göz RED: isim kuralı yeni bir ORTA getirdi; DÜŞÜK'ler).**

*ORTA — isim kuralı meşru adları reddediyor, kayıtlı adı kilitliyordu.* İkinci turun "her Cc ve Cf" kuralı panelde aşçı
emojisini (ZWJ), İskoçya bayrağını (etiket karakterleri U+E0020–E007F), Farsça ZWNJ'li adı, yapıştırılmış yumuşak tireyi,
LRM'yi ve baştaki BOM'u reddediyordu; panel formu adı her kayıtta yeniden gönderdiği için böyle adlı bir lokasyonun
IP/GPS/vardiya/SSID düzenlemesi kilitleniyordu. Kural **amaca** daraltıldı (`internal/domain/tenant/venue.go`):
- **reddedilen:** Cc (içte TAB/satır sonu dahil), U+2028/U+2029, bidi gömme/geçersiz kılma/yalıtım (U+202A–202E,
  U+2066–2069), U+200B, U+2060, içte U+FEFF;
- **izinli:** ZWJ, ZWNJ, etiket karakterleri, varyasyon seçicileri, her yazının harfleri (Arapça, İbranice);
- **sessizce atılan** (`CleanName`, panel ve API'nin ortak temizliği): U+00AD, U+200E/U+200F/U+061C (LRM/RLM/ALM) ve iki
  uçtaki BOM. Gerekçe: yapıştırılan metinde gelirler, bir adda anlamları yoktur; işaretler nötr karakterlerin sırasını
  oynatabilir — atmak riski alır ve yöneticiye göremediği bir karakteri aratmaz. Reddetmek, kimsenin yazmadığı bir karakter
  hakkında bir cümle olurdu.
- **Kayıtlı ad muaf:** kural yalnız **yeni ya da değişen** adı yargılar (`refuseHiddenName`, kayıtlı adı yazma işleminin
  içinde okuyup karşılaştırır — panelin `SaveVenue`/`SaveDepartment`'ı ve API'nin `PutVenueTx`'i). Panel sınırı (`parseName`)
  kuralı artık uygulamaz (kayıtlı adı bilmez); `tenant.ErrNameHiddenCharacter` el yordamıyla kendi cümlesine çevrilir.
  API'nin mesajı (`problemNameHidden`) ve OpenAPI açıklaması güncellendi.
Testler: `TestVenueSave_ANameOnFileStaysEditableAndANewHiddenOneIsASentence` (gerçek el yordamı + gerçek alan: U+202E'li
kayıtlı lokasyon ve departman aynı adla yeni aralık/vardiya alır; aynı karakterli **yeni** ad cümleyle döner; emoji ve
bayrak adlı yeni lokasyon kaydedilir), `TestLocationsPut_TheNameRuleJudgesOnlyWhatChanges` (API'de beş izinli biçim, üç
temizlenen biçim, kayıtlı adla etiket değişimi 200, yeni U+202E'li ad 422), `TestVenues_TheNameBoundIsEnforcedBeforeTheColumn`
(dokuz red, sekiz kabul). Mutantlar: "kayıtlı ad muaf" kolu kaldırılınca panel ve API testleri kırmızı; kural "her Cc+Cf"e
geri genişletilince emoji/bayrak/Farsça kırmızı.

*Kemer, ikinci delikler ve kopyalar (D2, D3).* Tenant kopyası artık parantezli `(SELECT …)`'i okur, sütun listesiz
`INSERT INTO t SELECT …`'i reddeder ve `tenant_id`'ye atama yapan `DO UPDATE SET`'i reddeder (üç sonda; her kolun kaldırılması
kırmızı). Öteki beş kopya (adminauth, billing, ledger, review, signup) kendi türetilmiş sorgularında koşan, kendi kendine
yeten bir INSERT … SELECT kolu taşır (`queryinsert_test.go`, olumsuz kontrolüyle). Ölçüldü: `CreatePasswordReset`,
`CreateAdminUser` ve `InsertTransactionReview` metinlerinde `tenant_id` değeri bozulunca üçü de kırmızı; teslim edilen
metinlerde 0 yanlış alarm. **Sınır:** bu beş kopya hangi tablonun tenant kapsamlı olduğunu bilmez — `tenant_id`'yi
adlandırmayan sütun listesini yargılamaz; DO UPDATE kolları yoktur (çağrılarında DO UPDATE yok).

*Tek başına UTF-16 surrogate kaçışı (D4).* `"Bar \ud800"` `encoding/json`'da U+FFFD olarak saklanıyordu; artık **400
`invalid_request`, `reason: invalid_utf8`** (ham değerde gönderilmemiş bir U+FFFD = bozuk kaçış). Kaçışla gönderilmiş
gerçek bir `\ufffd` kabul (kontrol). Mutant kırmızı.

*Deploy notu* plan §5 API-13'e yazıldı: deploy'dan önce prod'da `locations`/`departments` adlarında yeni kurala takılan
satır sayısı ölçülür (değer basılmaz). *(Dördüncü turda düzeltildi: bu paragraf "kayıtlı ad muaf, bu bir kilit değil"
diyordu; ölçümle yanlıştı — muafiyet kayıtlı adı **ham** karşılaştırıyordu, reddedilen **ve** atılan karakter taşıyan bir
ad (`Rusty<RLO><LRM>Bar`, `Ru<ZWSP>sty<BOM>`) hem panelde hem API'de 422 ile kilitliydi. Aşağıda.)*

**Dördüncü tur (2026-10-15 — üçüncü tur kısa kontrolü ONAY; kalan DÜŞÜK'ler, yeniden denetim yok, her değişiklik
ölçüldü).**

*Muafiyet temizlenmiş kayıtlı adla.* `refuseHiddenName` artık `name != CleanName(onFile)` karşılaştırır: kayıtlı ad, gelen
ad gibi temizlenerek "değişmedi" sayılır; ad bir sonraki kayıtta temizlenmiş hâliyle yazılır (ad değişikliği olarak iz
satırı bırakır). Ölçüldü: iki örnek (aynen ve LRM'siz geri gönderim; `Ru<ZWSP>sty<BOM>`) panelde kaydedilir, API'de 200;
ham karşılaştırma mutantı ikisinde de kırmızı. API-13 sayımına "temizlenince değişen" alt küme eklendi.

*Kayıt sihirbazı.* `signup.ValidateVenues` artık venue editörünün temizliğini (`tenant.CleanName`) ve karakter kuralını
uygular, kendi cümlesiyle ("hidden character"); yalnız atılabilir işaretlerden oluşan satır boş satırdır. Sihirbazdan
geçen bir ad sonsuza kadar muaf kalacaktı. Mutant (kural kaldırılınca U+202E, U+200B, BEL, U+2066 ve bağlamsız etiket
geçer) kırmızı. Sihirbazın altın dosyası yok; ekran metni değişmedi.

*Departman.* `TestVenueSave_…` departmanı yeni U+202E'li adla yeniden adlandırır → cümleli form, ad değişmez. Departmanın
yeniden adlandırma kuralı ve el yordamının departman kolu ayrı ayrı kaldırılınca kırmızı.

*Etiket karakterleri ve birleştiriciler bağlamında (isteğe bağlı madde — yapıldı).* U+E0020–E007E yalnız U+1F3F4'ten hemen
sonra başlayıp U+E007F ile kapanan dizide, U+E007F yalnız o kapanış olarak; ZWJ ve ZWNJ yalnız iki görünür karakter
arasında (uçta, çift, boşluk ya da gizli karakter yanında değil). Gerekçe: ikisi de görünmez metin taşıyabilir ("ASCII
smuggling"); bayrak, emoji dizisi ve Fars/Hint yazısı onları yalnız bu konumlarda kullanır. İskoçya/İngiltere/Galler
bayrakları, aile, aşçı ve gökkuşağı bayrağı (ZWJ'den önce U+FE0F) kabul. `TestNameHasHiddenCharacters_JoinersAndTagsOnlyWhereANameUsesThem`;
etiket ve birleştirici kolları ayrı ayrı kaldırılınca kırmızı. **Sınır (adlandırıldı):** varyasyon seçicileri (Mn) herhangi
bir karakterden sonra serbest; bir tabanın ardına dizilerek aynı yoldan veri taşıyabilirler — emoji sunumu U+FE0F istediği
için reddedilmedi.

*Kemer.* Altı kopyanın `INSERT … SELECT` başlığı tek dosyada (`queryinsert_test.go`, `insertSelectHeads`) ve sütun listesi ile
SELECT arasındaki CTE'yi (`WITH z AS (…) SELECT`) okur — beslenen SELECT, deyimin kendi derinliğindeki ilk SELECT'tir;
tırnaklı sütun adı da okunur. Tenant kopyasının `DO UPDATE` SET denetimi ham gövdeyi okur (blok yürüyüşü parantezleri
maskeler) ve tırnaklı `"tenant_id" =` ile satır biçimli `(…, tenant_id) = (…)`'ı da reddeder. Üç sonda; her genişlemenin
kaldırılması kırmızı. **Sınır:** `SET x = (SELECT tenant_id …)` gibi bir alt sorgu satır biçimi sayılabilir (yanlış alarm
yönünde); aynı değeri hesaplayan ifade hâlâ reddedilir, anlaşılmaz.

*Mesajlar.* Gizli karakter reddi artık `reason: hidden_character` (uzunluk `invalid`'den ayrılır); `problemNameHidden`
içteki U+FEFF'i ve bağlam kuralını sayar; OpenAPI ve test güncellendi.

*D4 kaçağı.* `brokenEscape` artık yalnız bir kaçışı başlatan ters bölünün ardındaki `ufffd`'yi sayar; `"\\ufffd \ud800"`
(kaçışlı ters bölü + düz metin, ardından eşsiz surrogate) 400. Eski sayım mutantı kırmızı.


## API-6 uygulama notu (2026-10-15)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda adıyla.
**Migration yok** (`invite_requests`, `employees`'in API sütunları ve K-24 indeksleri API-1'den; K-25'in sayımı
`audit_log_tenant_at_idx`'i kullanır). Mutantlar ve eşzamanlılık ölçümleri atılabilir bir PostgreSQL 17 kabında
(`scripts/db-init` ile, goose 52); paylaşılan `tappa-m11-db`'ye yalnız testlerin yazdığı satırlar.

**Parçalar.** Alan `internal/domain/tenant/staffapi.go` (panelin `*tenant.Staff`'ı — adres, ad ve iz kuralları
panelinki); sorgular `employees.sql` (dokuz), `audit.sql` (dört: K-25 kilidi ve sayımı, uyarının tekilleştirmesi ve
listesi), yeni `invitequeue.sql` (on iki); kuyruk işçisi `internal/invite/queue.go` (K-19 işçisinin işi); uçlar
`internal/handler/api/employees.go`; owner uyarısı panelin API anahtarları bölümünde.

**Sapmalar ve ADR'nin açık bıraktıkları — kararıyla (otonom, gerekçeli):**
1. **Gövde** (API-5'in sözleşmesi): `firstName`, `lastName` (zorunlu dize), `email` (zorunlu anahtar; dize ya da
   `null` = adres yok), `active` (zorunlu), `employeeCode` (isteğe bağlı, dize ya da `null`), `externalRef` (isteğe
   bağlı, yoldakine eşit). Bilinmeyen/yinelenen anahtar 422, UTF-8 olmayan gövde 400. `email`'in **anahtar olarak**
   zorunlu olması: PUT tam temsildir ve K-16'nın otomatik daveti adrese bağlıdır; "adres yok" açıkça `null` ile söylenir.
2. **Adres** panelin *değiştirme* kuralıdır (`deliverableEmail` → `mail.ValidRecipient`: ASCII, görünen ad yok, ≤ 254),
   ekleme formununki değil — API'nin adresi kuyruğun göndereceği adrestir. İşletmenin bir yöneticisinin adresi 422
   `administrator_address` (citext eşitliği; `GetInviteRecipient`'in sorusu istek anında). Başka **canlı** çalışandaki
   adres 409 `email_unavailable` — yeni satırda da adres değişikliğinde de; kısıt adıyla tanınır, işlem geri alınır
   (satır, istek, iz yok — ölçüldü). Adres hiçbir yanıtta, iz satırında ya da log'da yok (`same_email`/`has_email`
   SQL'de hesaplanan mantıksal değerler; kanarya testi).
3. **K-18 bağlaması** citext eşitliğiyle bulur; harf farkı **bağlandıktan sonra adres değişikliğidir** (K-15 uygulanır).
   Bağlama `EmployeeChanges` sınıfıdır (askıda kapalı). Holding eşleri aranmaz (K-24; testte başka işletmedeki aynı adres
   bağlanmadı).
4. **Ad kuralı** (brief'in sorusu — ölçüldü): API adı `CleanName` + panelin `personName`'i (boşluk birleştirme, boş ad,
   ≤ 120) + API-5'in karakter kuralı (`NameHasHiddenCharacters`, kayıtlı ad temizlenmiş hâliyle muaf). İçte kontrol
   karakteri (TAB/satır sonu dahil) ve bidi kontrolü **her zaman** `hidden_character` (00037'nin CHECK'i kontrol
   karakterini zaten reddeder; 23514 yerine cümle). `fullName` = `firstName + " " + lastName`, toplam ≤ 120 rün
   (rosterın gösterdiği ad; panelin sınırı). `employeeCode`: `CleanName`, 1..64, aynı karakter kuralı. **Ölçülen bulgu
   (değiştirilmedi, kapsam dışı):** panelin çalışan ekleme yolu (`personName`) BEL ve ESC'yi saklıyor, U+2060'ı ve
   bağlamsız ZWJ'yi kabul ediyor, TAB'ı boşluğa çeviriyor; U+202E'yi reddediyor. Kullanıcıya sorulacak madde olarak raporlandı.
5. **Yeniden işe alım:** canlı satırı olmayan ref'e `active:true` önce K-18'i dener, yoksa **yeni satır** (201; iz satırında
   `rehire_of` = önceki satır). `active:false` hiç tutulmamış ref'e 404; yalnız pasif satırı olan ref'e satır olduğu gibi
   (200, bütçe harcanmaz, `EmployeeChanges`). `DELETE` aynı.
6. **K-25:** sayım `audit_log`'dan — `action='employee.deactivated'`, `actor_id` = anahtar, `detail->>'via'='api'`,
   son bir saat; anahtarın kimliğiyle ad alanlı `pg_advisory_xact_lock`, **satır kilitlerinden önce** (tek kilit sırası:
   anahtar, sonra satırlar). Aşımda **429 `deactivation_budget_exceeded`** (`rate_limited`'dan ayrı kod: entegratör kova
   kısıtlamasını bütçeden ayırt edebilsin), `Retry-After` = en eski sayılan satırın pencereden çıkışına kalan saniye.
   `api.deactivation_budget_exceeded` satırı reddedilen işlem geri alındıktan sonra kendi işleminde, **anahtar başına
   saatte bir** (aynı kilit altında tekilleştirilir — her ret bir satır yazsaydı ısrarcı bir entegrasyon izi doldururdu);
   hedef ve aktör anahtarın kimliği, ayrıntı `{via, budget, window_seconds}`. Panel uyarısı API anahtarları bölümünde
   (owner-only), son 24 saat, anahtarın şimdiki adıyla.
   **Ölçümler** (`internal/domain/tenant`, buluşma noktalı — soğuk havuz yarışçıları sıraya dizip kilitsiz mutantı
   gizliyordu, ilk denemede ölçüldü): bir hak kala 12 eşzamanlı → tam 1; bütçe 5, 12 eşzamanlı → tam 5. Mutantlar:
   kilit kaldırılınca 10/2 ve 12/0 (kırmızı); iz satırı **commit'ten sonra** yazılınca 4/8 (kırmızı); süreç belleğinde
   sayaç → "yeniden başlatma" kolunda bütçe sıfırlanır (kırmızı). *Adlandırılmış:* iz satırını **kilit içinde** ayrı bir
   işlemde yazmak yeşil kalır — o işlem kilit bırakılmadan commit eder, ihlal değildir; ADR'nin cümlesi "kilit bırakıldıktan
   sonra görünen iz"i hedefler.
7. **Davet kuyruğu (K-17)** — `internal/invite/queue.go`:
   - Talep **tek tek** alınır (`ClaimInviteRequest`: en eski vadesi gelmiş satır, `FOR UPDATE SKIP LOCKED`, kira
     `clock_timestamp() + 60 s`, `attempts + 1`); toplu talep, sırayla gönderilirken sonrakilerin kirasını bitirirdi.
   - Basım işlemi: durum okuması → panelin tavanları (`checkLimits`, aynı kilit ve sayaçlar) → eski davetlerin emekliye
     ayrılması → çalışan `FOR SHARE` (yalnız `invited`'a gönderilir) → `CreateInvite` → adres okuması + dört ret +
     kutu tavanı → **son ifade olarak kiranın yenilenmesi** (`lease_until = @talep edilen` ile; satır yoksa basım geri
     alınır). Yenilenen kira basımdan 60 s — gönderim (10 s) + iz (5 s) bütçesini kapsar; istek kuyruktan **yalnız bu
     kirayla** çıkar (`FinishInviteRequest`).
   - Tavan ya da kutu tavanı → istek kalır, 10 dk sonra vadeli (ret yok, `invite.email_refused` yok); başarısız gönderim
     → 1/5/15 dk geri çekilme, 5 denemeden sonra bırakılır (her deneme `invite.undelivered` yazar). *(İkinci ve üçüncü
     turda değişti — aşağıda: tavanlar ve kesici basımdan önce sorulur, 4xx bir denemedir, 72 saatlik ömür sınırı,
     K-22 kapısı.)* Adresin kalıcı
     retleri (adres yok, ASCII değil, röle almıyor, yöneticinin) → `invite.email_refused` + istek düşer.
   - Kuyruğun iz satırları `detail.channel = "email_queue"` (panelin basışı `"email"`), aktör NULL (sistem — bir isteğin
     değil işçinin eylemi; K-23 API isteklerinin satırları içindir).
   - İşin "sonra gel" cevabı `*invite.Later` (`webhook.DueLater` arayüzü): işçi o tenant'ı gecikme sonunda yeniden
     görür (o tenant'a bir zamanlayıcı, tarama yok — K-19). Yalnız `Later` dönen iş hata sayılmaz; başka hatayla
     birleşmişse log'a yazılır. `webhook.Worker.Add` (Start'tan önce) eklendi; `cmd/tappa` işçiyi kuyruk eklendikten
     **sonra** başlatır.
   - **Ölçümler:** kuyrukta kod yok — şema beş sütun ve tipleri; işçi `webhook.Worker` üzerinden yakalanan log ile
     koşarken kod, hash'i ve adres log'da, işletmenin iz satırlarında ve `invite_requests` satırlarında yok (başarısız
     gönderimin log'a düştüğü doğrulanarak). İki "pod" 10 isteği aynı anda boşaltır → herkese tam 1 davet, 1 e-posta.
     Kirası dolan pod: A basımın ortasında durur, kira geçmişe çekilir, B talep eder ve bekler (A işletmenin tavan
     kilidini tutuyor), A'nın yenilemesi B'nin kirasını görür → A'nın basımı geri alınır: 1 davet, 1 e-posta. Mutantlar:
     yenileme kontrolü kaldırılınca 2/2 (kırmızı); talepten kira koşulu kaldırılınca podlar birbirinin kirasını çalar,
     kimseye davet basılmaz (kırmızı — çift basım değil canlı kilit). *Adlandırılmış:* `SKIP LOCKED` → `FOR UPDATE`
     yeşil (bekleyen, EvalPlanQual ile kirayı görür ve satırı almaz): `SKIP LOCKED` güvenlik değil verim içindir;
     güvenlik kira koşulu + yenilemedir.
8. **K-15 yarışı** (brief'in "işçi tam basarken e-posta değişiyor"u): işçi basım işleminin içinde (ekleme ve adres okuması
   yapılmış) durdurulur; API'nin adres değişikliği çalışan kilidinde bekler (`pg_stat_activity`'de `LockEmployeeForAPI`
   bekleyişi görülür); işçi commit eder ve eski adrese gönderir; değişiklik o daveti **kendi işleminde** emekliye ayırır
   ve isteği serbest bırakır (`RequeueInviteRequest`: `lease_until = NULL` — işçi isteği kuyruktan çıkaramaz); işçinin
   aynı koşusu yeni adrese basar. Eski bağlantı, işçinin ikinci basımı **commit edilmeden** okunur — `ErrCodeCancelled`.
   Mutantlar: değişikliğin ikinci emekliye ayırması kaldırılınca bağlantı canlı (kırmızı; testin ilk hâli işçinin ikinci
   basımının emekliye ayırmasıyla maskeleniyordu — okuma o basımın commit'inden önceye alındı); yeniden kuyruğa alma
   `ON CONFLICT DO NOTHING` olunca işçi isteği bitirir, yeni adrese davet gitmez (kırmızı). *Adlandırılmış:* değişikliğin
   `FOR UPDATE`'i `FOR NO KEY UPDATE`'e çevrilince ya da işçinin `FOR SHARE`'i kaldırılınca **tek başına** yeşil — iki kilit
   birbirine karşı ayrı ayrı yeterli; ikisi birlikte kaldırılınca değişiklik hiç beklemez (kırmızı).
9. **resend-activation:** canlı istek **ya da** son bir saatte basılmış harcanabilir davet varken etkisiz (202, iz yok);
   istek anında hiçbir davet emekliye ayrılmaz (bir saat sonraki resend'in kuyruğa aldığı anda bile eski bağlantı canlı —
   ölçüldü; emeklilik işçinin basımında). Aktive çalışan 409 `already_activated`; adresi olmayan 422 (`field: email`,
   `reason: required`); canlı satırı olmayan ref 404. Kuyruğa alan resend `invite.requested` yazar. Mutant (istek anında
   emekliye ayırma) kırmızı.
10. **Askı sınıfları** (§10): yeni satır (yeni kişi ve yeniden işe alım) `WriteEmployeeCreate`; var olanı değiştiren,
    bağlayan, değişmeyen ve `active:false` PUT `WriteEmployeeChange`; DELETE `WriteEmployeeDelete`; resend
    `WriteResendActivation`. PUT'un sınıfı aramadan sonra belli olduğu için alan, kontrol noktasını (`tenant.Admit`)
    yazmadan önce sorar; eşzamanlı oluşturma ON CONFLICT'e düşerse ikinci kez `Change` ile sorar. Mutant (her PUT `Create`)
    kırmızı.
11. **K-19 bildirimi commit'ten sonra:** zincirin bildirimi yazmadan önce geldiği için, kuyruğa alan yazma commit'ten sonra
    tenant'ı yeniden bildirir. Mutant (bildirim kaldırılınca) `cmd/tappa` uçtan uca testinde 5/5 kırmızı.
12. **GET:** `/v1/employees` işletmenin **bütün** çalışanlarını (canlı/pasif, panelde açılmış ve henüz bağlanmamış —
    `externalRef: null`) `id` sırasıyla sayfalar; imleç `emp_<uuid>`, yalnız `limit` ve `cursor` (başka parametre 400).
    `/v1/employees/{ref}` canlı satırı, yoksa en son pasif satırı verir. `activationEmail.status` kapalı küme `queued` /
    `sent` / `none` (`sent`: harcanabilir bir davet var — panel modunda ekranda gösterilen bağlantı da), `at` zamanı.
13. **Teslim modu:** kuyruk yalnız `TAPPA_INVITE_DELIVERY=email`'de kurulur (e-posta taşıyıcısı ve panelin kutu tavanı
    örneğiyle); `panel` modunda kapalıdır, açılış satırı söyler, istekler bekler (`activationEmail: queued`) ve owner
    bağlantıyı panelde gösterir. Üretim `email`'dedir (ADR 0022 EM-5B notu); sandbox (`panel`) için API-11 karar verir.
14. **Eşzamanlı değişiklik:** arama ile kilit arasında satır pasifleşir ya da ref'i değişirse `ErrEmployeeChangedConcurrently`
    → 503 (yeniden denenebilir); hiçbir şey yazılmaz.
15. **Kemer (§4.5) ve iki sorgu biçimi** — tam koşuda yakalandı, kemer gevşetilmedi, sorgular düzeltildi:
    (a) `CreateEmployeeFromAPI` önce `ON CONFLICT (tenant_id, external_ref) WHERE status <> 'deactivated' DO NOTHING`
    taşıyordu (API-5'in biçimi; kısmi indeksin hakemi yüklemini ister) ve tenant kopyası o sondaki `WHERE`'i ifadenin
    kendi WHERE'i olarak okudu (kırmızı). Artık düz `INSERT … SELECT … FROM tenants t WHERE t.id = @tenant_id`; ekleme bir
    **SAVEPOINT** içinde koşar, `employees_tenant_external_ref_key` adlı 23505 savepoint'e geri alınıp eşzamanlı oluşturma
    olarak sürer (`TestEmployeesPutDomain_ConcurrentNewRefOpensOneRow`: tutulan commit edilmemiş satır, dört PUT bekler →
    commit'te 0 oluşturma, geri almada tam 1; tek canlı satır. Mutant — kol kaldırılınca yarışan PUT 23505 ile düşer —
    kırmızı). (b) K-25'in kilidi çıplak bir `pg_advisory_xact_lock` iken kemerin sorgu başına anti-vakuite kontrolü "hiçbir
    tenant tablosu okunmadı" dedi; kilit artık çağıranın `tenants` satırını okuyan sorgunun **seçim listesinde** (WHERE'in
    kabul ettiği tek satır için bir kez; bağlam satırı göremezse satır yok, kilit yok — hata sayılır).
16. **Store envanteri** (`cmd/tappa/storekeyshape_test.go`): 25 yeni yöntem kayda girdi (adres, kod, hash döndüren yok);
    sayılar 22 → 23 dosya, 159 → 184 sorgu, 23 → 24 üretilmiş dosya.

**Uçtan uca.** `TestAPIWiring_AnEmployeeRunsThroughTheShippedSurfaceAndTheQueue`: `openAPISurface` panelin `tenant.NewStaff`'ıyla,
K-19 işçisinin işi komutun kurduğu gibi kuyruk (`invite.NewQueue` + `EmailInvitations.QueueSink`, yakalayan gönderici); API
host'unda 201, işçi commit'ten sonra uyanır, şablondan render edilmiş **tek** e-posta satırdaki adrese gider, bağlantısı
`Lookup` ile çalışanın ilk aktivasyonuna (ev lokasyonu yok — K-14) çözülür; ADR 0026'nın onay + ilk dokunuş yolu
`TestInviteQueue_TheLinkActivatesThroughTheWizardsPath`'te (`RecordConsent` + `Activate` → `active`). Ana host'ta 404.

**İkinci tur (2026-10-15 — iki mercek RED: aynı ORTA çifti; kırmızı çizgi ihlali yok; iki kullanıcı kararı).**

*O-1 — "gönderildi" teslime bağlandı.* Basım commit edildikten sonra gönderim başarısız olursa davet canlı ama
gönderilmemiş kalıyordu; vazgeçmede istek siliniyor, davet emekliye ayrılmıyordu — `GET` "sent" diyor, resend etkisiz
kalıyordu (ölçüldü). Artık "sent" = harcanabilir **ve** teslim satırı olan davet: `invite.code_emailed` (panel modunda
`invite.code_shown_to_manager`) satırının `detail.invite_id`'si onu adlandırır — `activationEmail` ve resend'in "son 1
saatte gönderilmiş" kontrolü (`CountRecentSentInvites`) bunu okur. Yeni durum **`undelivered`**: kuyrukta istek yok,
teslim edilmiş canlı bağlantı yok ve kişinin izdeki **son** teslim denemesi `invite.undelivered` ya da
`invite.email_refused` (öncelik: `queued` > `sent` > `undelivered` > `none`; OpenAPI güncellendi). Kuyruk vazgeçince
bastığı ve hiç teslim etmediği daveti **id ile** emekliye ayırır (`RetireUndeliveredInvite` — `cancelled_at`'i yazan ikinci
ifade; `invites.sql` başlığı güncellendi) ve isteği aynı işlemde siler. *Adlandırılmış:* süresi dolan bir gönderim veriden
sonra kabul edilmiş olabilir (ADR 0022 sınır 15); vazgeçmede o bağlantı da ölür — owner `undelivered` görür, resend yenisini
açar. *Owner'ın panel yüzeyi:* çalışan listesi bugün davet durumu göstermiyor (ölçüldü: roster sorgusu `employee_invites`'ı
okumaz); `undelivered` API'de ve izde (`invite.undelivered` satırları) görünür — panelde göstermek ayrı bir iştir (yeni yüzey,
§9 dışında ama `tappa-brand`li bir ekran değişikliği).
Testler: `TestInviteQueue_GivingUpRetiresTheUnsentLinkAndSaysUndelivered` (beş 550 → vazgeçme, bütün bağlantılar ölü,
`undelivered`; arada `queued`; resend yeni istek açar; teslim edilen gönderimden sonra `sent`; panelin e-postası başarısız
basışı → `undelivered`, resend etkisiz değil). Mutantlar: vazgeçmedeki emekliye ayırma kaldırılınca gönderilmemiş bağlantı
canlı; "sent" teslim satırı olmadan okununca panelin başarısız basışı "sent" ve resend etkisiz — ikisi de kırmızı.

*O-2 — tavan deneme hakkı harcamaz.* Talep artık deneme saymaz; yalnız gerçek gönderim hatası sayar (`FailInviteRequest`).
Kutu tavanı (basım işleminde sorulan ve gönderimde dönen), süreç kesicisi ve 4xx kısıtlama (`mail.ClassRecipientCap`,
`ClassBreaker`, `ClassThrottled`) isteği 10 dk erteler, sayaç artmaz. *(Üçüncü turda düzeltildi: 4xx yeniden deneme
sayılır ve ertelemelerin bir sınırı var — aşağıda O-B.)* Gerçek hatada 1/5/15 dk geri çekilme ve 5 deneme kaldı.
`TestInviteQueue_ACeilingDelaysAndSpendsNoTry`: altı tavan reddi → 0 deneme (her birinden sonra API `queued`), bir 550 → 1.
Mutantlar: tavan sınıfları hata sayılınca ve talep yeniden deneme sayınca — ikisi de kırmızı. *Not:* sıkıştırılmış testte
ADR 0022 §9'un kişi başına saatlik 3 basım tavanı bir sonraki denemeyi geciktirir (o da erteler); test önceki davetleri sahip
bağlantısıyla iki saat geri alır.

*`audit_log.at` — migration `00053`.* K-25 ile güvenlik yükü taşıyan `at`'i `tappa_app` tablo düzeyi INSERT ile yazabiliyordu
(iki mercek ölçtü). Ürünün tek INSERT'i (`RecordAuditEvent`) sütunu adlandırmıyor; tanımlayıcıların `at` yazması (`00034`,
`00039`, `00042`) kendi rollerinin sütun yetkisiyle — dokunulmadı. `tappa_app`'in INSERT'i `(id, tenant_id, actor_id,
action, target, detail)`'e daraltıldı; son koşul **etkin** yetkiyi okur. `TestAuditLog_TheAppCannotWriteTheTime` (`at`
adlandıran INSERT 42501; ürünün INSERT'i ve `id` adlandıran yazar), `TestAuditLog00053_DownGivesBackAndUpTakesItAgain`.
Mutant (00053 geri alınınca) kırmızı. Tarih yazan testler sahip bağlantısına taşındı. `opAtVersion` 53'ü atlar.

*K-25 penceresi.* `TestDeactivationBudget_ComesFromTheDatabase` iki kenarı pinler: 61 dakikalık üç satır yer bırakır, 59
dakikalık üç satır bütçeyi doldurur. Pencereyi 1 dakikaya indiren mutant artık kırmızı.

*Kullanıcı kararı 1 — panelin çalışan adları.* Panelin ekleme yolu (`Staff.Add`) API-5'in lokasyon kuralını uygular
(`CleanName` + `NameHasHiddenCharacters` → `ErrNameHiddenCharacter`, lokasyon editörünün cümlesi), sonra `personName`.
Panelde bir kişinin adını değiştiren yol yok — kayıtlı ad muafiyetinin uygulanacağı bir şey yok (adlandırıldı).
`TestStaffAdd_TheNameRuleIsTheVenueRule` (sekiz red, dört kabul), `TestEmployeeAddDB_AHiddenCharacterIsASentence`.
Mutant (kontrol kaldırılınca BEL, ESC ve TAB saklanır) kırmızı. Ölçüldü: `seed.sql`'de kurala takılan ad 0; paylaşılan
geliştirme veritabanında 30 120 çalışan satırından 25 ad (test fikstürleri; kayıtlı, yeniden yargılanmaz).

*Kullanıcı kararı 2 — ana sayfa bandı.* Panelin ana sayfası (`/admin`) canlı bir owner'a son 24 saatte bütçesini aşan bir
anahtar varsa küçük bir uyarı (`ToneWarn`, anahtar bölümüne bağlantı) gösterir; okuma handler'da ve alanda
(`apikey.Keys.BudgetAlerts`) owner'a özel, okunamazsa bant yok. Bant yokken sayfa bayt bayt aynı
(`TestKeyBudgetBanner_AbsentAddsNothing`); `TestPanelHome_TheKeyBudgetBannerIsTheOwnersAlone`. Mutant: iki owner kontrolü
birlikte kaldırılınca manager bandı görür (kırmızı); handler'ınki tek başına kaldırılınca alan reddeder (yeşil —
adlandırıldı). Anahtar bölümündeki "20" artık satırın `detail.budget`'ından okunur.

*Adlandırılan bedeller (DÜŞÜK'ler):*
- **Kira dolduktan sonra çift gönderim.** Gönderici kirayı (60 sn — gönderim 10 sn, iz 5 sn bütçesiyle) aşacak kadar asılırsa
  başka bir pod isteği yeniden alıp basar ve gönderir: iki e-posta gider, ama ikinci basım birincinin bağlantısını emekliye
  ayırdığı için **canlı bağlantı tektir**. Gönderim bağlamı süreyle sınırlı olduğu için bu ancak süreyi dinlemeyen bir
  taşıyıcıda olur.
- **Tenant içi kehanet (güvenlik D-2).** 409 `email_unavailable`, 422 `administrator_address` ve K-18 bağlaması, anahtar
  sahibine **kendi işletmesinde** bir adresin bir çalışana ya da yöneticiye ait olduğunu doğrular. Anahtar zaten o işletmenin
  çalışanlarını yazan yetkidir; başka tenant'a dair bir şey söylenmez (K-24). Plan §5 API-12 rehber notuna yazıldı.
- **Anahtar iptali kuyruğu temizlemez (güvenlik D-3).** `invite_requests` isteği hangi anahtarın açtığını tutmuyor (beş
  sütun, §9); ayırt etmek bir migration ve sütun ister. Kuyruğu topluca silmek meşru davetleri de keserdi — seçilmedi.
  *(Üçüncü turda daraldı:)* operatörün API'yi kapatması kuyruğu artık **durdurur** (K-22 basımdan önce okunur — aşağıda O-A);
  kalan bedel yalnız anahtar iptalidir: API açıkken, iptal edilen bir anahtarın açtığı istekler tavanlar ve 72 saatlik ömür
  içinde gitmeye devam eder; owner o çalışanları panelden pasifleştirebilir ya da operatörden kapatmayı isteyebilir.

**Üçüncü tur (2026-10-15 — iki mercek RED: iki ORTA; ikinci turun maddeleri kapandı).**

*O-A — davet kuyruğu K-22'yi okumuyordu* (ölçüldü: `api_enabled_at` NULL bir tenant'ta 1 gönderim, durum `sent`; tap'in
bildirimi de kapalı tenant'ın kuyruğunu işletiyordu). §2 kapıyı "her yolda", işçinin gönderim anı dahil ister. Basım işlemi
artık kişinin durumundan sonra `tenants.api_enabled_at`'i okur: kapalıysa hiçbir şey basılmaz ve gönderilmez, deneme
harcanmaz, istek **silinmez**, 15 dk sonra yeniden vadelidir (`queued` kalır — durumun anlamı "bekliyor"dur, yeni bir değer
eklemek entegratöre kendi kapatılmamış API'sinde görünmeyecek bir hâli anlatırdı); yeniden açılınca kaldığı yerden gider. *Adlandırılmış yarış (dördüncü tur D-c):* kapı basım işleminde **kilitsiz** okunur; o okumadan sonra commit edilen bir
kapatma, uçuştaki tek gönderimi durdurmaz — kapanıştan sonra en çok bir e-posta gider (denetim ölçtü); sonraki her koşu kapıyı
yeniden okur. `tenants` satırını kilitlemek her basımı operatörün yazmasıyla sıraya sokardı; bir e-posta için değmez.
`TestInviteQueue_AClosedAPISendsNothingAndWaits` (kapalı → 0 basım, 0 gönderim, istek duruyor, `queued`; açılınca gönderilir).
Mutant (kapı kaldırılınca kapalı tenant'a gönderim) kırmızı. Kuyruk testlerinin işletmesi artık sahip bağlantısıyla açılır.
*Sınır:* 72 saatten uzun kapalı kalan bir işletmenin istekleri açıldığında ömrünü doldurmuş olur ve `undelivered`'a düşer
(resend yeniden ister).

*O-B — ertelemelerin sınırı yoktu ve basım sonrası erteleme tavanları yakıyordu* (ölçüldü: 25 × 450 → 25 kod, 25 e-posta,
25 iz satırı, hep `queued`; dört kesici döngüsü panelin kişi tavanını doldurdu). ADR 0022 §9'un tavanları **basımları**
sayar (`CountRecentInvites` `employee_invites` satırlarını sayar — kod okuması ve testle ölçüldü). Üç değişiklik:
(a) basımdan **önce** sırayla: tavanlar, adresin kalıcı retleri, kesici (`RecipientCap.Refusing` → `Breaker.Refusing`; kuyruğun
kendi `invite.QueueSink` arayüzü) ve kutu tavanı — ret basım yapmaz, sayaç harcamaz, deneme harcamaz; adres basımdan sonra
yeniden okunur ve gideceği yerin otoritesi o kalır; (b) sınıftan bağımsız **72 saatlik ömür**: `requested_at` daha eskiyse
vazgeçilir — kişinin teslim edilmemiş bağlantıları emekliye ayrılır (`RetireUndeliveredInvites`: teslimi **kaydedilmiş** bir bağlantıya
asla dokunmaz — dördüncü turda daraltıldı, aşağıda D-g; ikinci turun id'li `RetireUndeliveredInvite`'ının yerini aldı), `invite.email_refused` `reason:
"request_expired"` yazılır, istek çıkar, API `undelivered` der; (c) **4xx bir denemedir** (gerekçe: `internal/mail` bir 4xx'i
gönderimin içinde bir kez yeniden dener; burada görülen ikinci ret; erteleme sayılınca bir alan adının her alıcıya 450
vermesi isteği ömür boyunca canlı tutuyor, her denemede kod basıyordu). Gönderimde dönen kesici/kutu reddi (basım öncesi
soruyla bir yarış) hâlâ ertelemedir ve ömürle sınırlıdır. **"5 deneme" taahhüdü:** gerçek gönderim hatası (5xx, ağ, zaman aşımı,
4xx) 5 kez; tavan ve kesici ertelemeleri deneme saymaz, onları ömür sınırlar.
Testler: `TestInviteQueue_ACeilingDelaysAndSpendsNoTry` (basım öncesi kutu tavanı ve kesici: 0 basım; gönderimdeki kutu/kesici
reddi: 0 deneme; bir 550: 1), `TestInviteQueue_AnOpenBreakerMintsNothingAndSparesThePanelsCount` (beş koşu → 0 basım; ardından
panelin basışı geçer), `TestInviteQueue_AThrottlingDomainEndsUndelivered` (5 × 450 → `undelivered`, canlı bağlantı yok),
`TestInviteQueue_ALifetimeEndsEveryRequest` (73 saatlik istek → `request_expired`, `undelivered`). Mutantlar, her biri kırmızı:
kesicinin basım öncesi sorusu kaldırılınca 3 basım ve panelin kişi tavanı doldu; 4xx yeniden erteleme sayılınca istek kuyrukta
kaldı; ömür kontrolü kaldırılınca istek bekledi.

*D-1 — bant yokken ana sayfa 1 bayt uzundu* (sayfa gövdesindeki `if`'ten sonra templ bir ayırıcı boşluk yazıyordu: 3395 →
3396). Koşul bant bileşeninin içine alındı ve bileşen plaket uyarısına `templ.Join` ile bağlandı (iki satırlık ardışık `@`
çağrısı da boşluk yazıyordu — ölçüldü). `TestKeyBudgetBanner_AbsentAddsNothing` artık bantsız sayfayı `c4f6f74`'ün (bant
öncesi) şablonundan render edilmiş iki altın dosyayla (`internal/handler/testdata/key-budget-golden/`) karşılaştırır.
Mutant (`if` sayfa gövdesine dönünce 3396 bayt) kırmızı.

*D-2 — liste ve tekil okuma aynı dört durumda pinlendi:* `TestAPIEmployees_TheListAndTheSingleReadAgree` (teslim edilmiş canlı
bağlantı `sent`; tek satırı `undelivered` olan canlı bağlantı `undelivered`; `email_refused` → `undelivered`; teslim edilmiş canlı
bağlantı + daha yeni başarısız deneme → `sent`). Önceki turda yeşil kalan üç mutant (listede teslim kontrolü yok;
`last_undelivered` sabit false; tekil okumada `email_refused` yok) artık kırmızı.

*D-3 — performans, migration `00054`.* Liste her sayfada işletmenin bütün izini okuyup sıralıyordu (denetim 127–156 ms
ölçtü; bu kapta 51–58 ms). Liste önce sayfayı seçer (CTE), son teslim denemesini ve teslim edilmiş canlı daveti satır başına
`LATERAL` ile okur. `00054`, `audit_log (tenant_id, target, at)` üzerinde dört teslim eylemine **kısmi** bir indeks ekler;
tekil okumanın teslim join'i kişiye bağlandı. Ölçüldü (atılabilir PostgreSQL 17; 2 000 çalışan, 240 000 iz satırı): 500'lük
sayfa **6,3–6,8 ms** (1 000: 13 ms); aynı sorgu indeks devre dışıyken 75–125 ms; tekil okuma 3,6 → 0,2 ms.
`TestAuditLog00054_TheDeliveryIndexIsPartialAndGoesBack` indeksin biçimini ve Down/Up'ını pinler; süre bir plana bağlı olduğu
için pinlenmez. *Bedel:* indeks goose'un işleminde (`CONCURRENTLY` değil) kurulur ve kurulum süresince `audit_log` yazmalarını
bekletir — API-13 runbook'u tablonun boyutunu deploy'dan önce ölçer.

*D-4 — bozuk `detail.budget` 500 veriyordu.* `ListAPIBudgetAlerts` bütçeyi yalnız en çok dokuz haneli bir JSON sayısıysa okur,
değilse 0 (limit gösterilmez). `TestApiKeyPanel_AMalformedBudgetIsReadAsNone` (metin, 2.5, -3, on bir hane ve 9: bölüm 200, beş
anahtar, tek limit; bant çizilir). Mutant (çıplak `::int`) 500 — kırmızı.

*Adlandırılan bedel — kaydedilemeyen teslim.* Gönderim kabul edilip `invite.code_emailed` satırı yazılamazsa (`ErrNotRecorded`;
işçi log'a yazar) davet `sent` görünmez: istek kuyruktan çıkar, durum `none` ya da önceki denemenin `undelivered`'ıdır, ve bir
resend ikinci bir e-posta gönderip ilk bağlantıyı emekliye ayırır. Teslimin kanıtı iz satırıdır; satırsız bir gönderim
kanıtsızdır.

**Dördüncü tur (2026-10-15 — üçüncü tur iki mercekten ONAY; kalan DÜŞÜK'ler, yeniden denetim yok, her değişiklik ölçüldü).**

*D-a — ömür kapıdan önce.* Kapalı kontrolü ömürden önce geldiği için kapalı bir işletmenin kuyruğu 15 dakikada bir sonsuza dek
işleniyordu; plan §5 API-12 "en çok 72 saat" diyor. Ömür artık K-22 kapısından **önce** yargılanır: kapalıyken de 72 saatte
`undelivered`. `TestInviteQueue_AClosedAPIStillEndsAtTheLifetime`; mutant (sıra değişince `*Later`, istek kalır) kırmızı.

*D-d — ömür ve vazgeçme pinlendi.* `TestInviteQueue_ALifetimeEndsEveryRequest` artık alt kenarı (71 saatlik istek bekler) ve
vazgeçmenin emekliye ayırmasını (önceki bir koşunun bastığı, gönderimi kesicinin reddettiği bağlantı vazgeçmeden sonra ölü)
pinler. Mutantlar — `queueLifetime` bir dakika, `giveUp`'tan emekliye ayırma kaldırılınca, sıra değişince — her biri kırmızı.

*D-b — `00054`'e `lock_timeout`.* Ölçüldü (denetim): açık bir `audit_log` işleminin arkasında `CREATE INDEX` 5,3 sn bekledi,
ardındaki iz yazması 3 sn'de düştü; `op_remove_holding_member` `audit_log`'u beklerken üyelik satırını tutar (ADR 0027 §13
madde 16). `00054` henüz yayınlanmadığı için yerinde değişti: Up ve Down ilk ifade olarak `SET LOCAL lock_timeout = '1s'`
(API-2 emsali). `TestAuditLog00054_GivesUpAfterASecondBehindAnOpenWrite`: `00054`'ün kendi Up bölümü açık bir `audit_log`
INSERT'inin arkasında 1,06 sn'de 55P03; mutant (`SET LOCAL` kaldırılınca 4 sn bekler) kırmızı. `deploy/README.md`'nin
`lock_timeout` bölümüne eşikle yazıldı (~1 M satır ≈ 0,8 sn; ~2,5 M satırın üstünde `CONCURRENTLY` ile ayrı migration).

*D-e — liste koruması.* `TestAPIEmployees_TheListAndTheSingleReadAgree`'ye iki kişi: E (önce başarısız, sonra teslim edilmiş ama
süresi dolmuş deneme → `none`) ve işletmenin en yeni iz satırının sahibi G (başarısız → `undelivered`). Yeşil kalan mutantlar —
listede kişi yüklemi (`la_a.target = e.id`) kaldırılınca; son deneme eskiden yeniye okununca, listede ve tekil okumada — artık
kırmızı. D-4'ün sınırları: `"20"` (JSON dizesi) ve on haneli `1234567890` limit göstermez; `jsonb_typeof` kaldırılınca ve hane
sınırı ona çıkınca kırmızı (`TestApiKeyPanel_AMalformedBudgetIsReadAsNone`).

*D-f — toplu davet ve ömür (karar).* ADR 0022 §9'un işletme tavanı günde 300 basım; tek seferde ~900'den fazla çalışan açan bir
senkronda kalanlar 72 saatte `request_expired` olur ve API onları `undelivered` olarak gösterir. Nedeni (`request_expired` mi,
röle reddi mi) API'de **ayrıştırılmadı**: iki durumda da çare aynıdır (resend — adresin kendisi PUT'ta zaten 422 ile
doğrulanır, kalan ret nedenleri röle ya da kutu kaynaklıdır ve entegratörün düzeltebileceği bir alan değildir); yeni bir alan
(`activationEmail.reason`) kapalı bir neden sözlüğünü dış sözleşmeye taşır ve iz satırının `detail`'ini API yanıtına bağlardı.
Plan §5 API-12 rehber notuna yazıldı: büyük ilk senkron saatlere ve günlere yayılır; 72 saatte gitmeyen `undelivered` olur,
resend yeniden ister.

*D-g — "teslim edilmiş" değil "kaydedilmiş teslim".* `RetireUndeliveredInvites` teslim satırı yazılmış bağlantıya dokunmaz;
panelin basıp commit ettiği ama e-posta satırını henüz yazmadığı (gönderim commit'ten sonradır) bir bağlantı o anda
teslimsiz görünür ve kuyruk tam o anda vazgeçerse emekliye ayrılır. Pencere tek bir gönderimin süresidir (≤ 10 sn); çare resend
ya da yeni bir panel basışıdır. Cümle ve `invites.sql` yorumu daraltıldı.


## API-7 uygulama notu (2026-10-15)

Yapıcının uygulaması ve ölçümleri; kararlar değişmedi — sapmalar ve ADR'nin açık bıraktıkları aşağıda adıyla.
**Migration yok** (`punch_events`, `feed_counters`, `punch_events_pkey (tenant_id, seq)` ve `tappa_app`'in SELECT'i
API-2'den). Mutantlar ve ölçek ölçümü atılabilir bir PostgreSQL 17 kabında (`scripts/db-init` ile, goose 54); paylaşılan
`tappa-m11-db`'ye yalnız testlerin yazdığı satırlar.

**Parçalar.** Sorgular `db/queries/punchfeed.sql` (iki: `GetPunchFeedHead`, `ListPunchEvents` — ikisi de açık
`tenant_id` taşır; `internal/domain/tenant`'ın §4.5 kemeri ikisini de okur); okuyucu `internal/domain/tenant/punchfeed.go`
(`tenant.PunchFeed`, `WithTenant(anahtarın tenant'ı)`); uç `internal/handler/api/punches.go`; kablolama `openAPISurface`'te
tek bir `tenant.NewPunchFeed(data)` (panelden paylaşılacak kural ya da iz yok — okuma). `transactions` adlandırılmaz:
`TestEvidenceReaders_EveryOneIsClassified`'ın sınıflandıracağı yeni okuyucu yok.

**Sözleşme ve ADR'nin açık bıraktıkları — kararıyla (otonom, gerekçeli):**
1. **`scope` zorunlu, kapalı küme** (`employer` | `location`; §2'nin cümlesi: hedef = anahtarın tenant'ı ∧ satırın rolü).
   Varsayılan bir kapsam plan §2 madde 31'in uyarısını sessiz bir veri hatasına çevirirdi. Tek-tenant punch iki kapsamda.
2. **Sorgu parametreleri API-5'in gövde kuralıyla:** yalnız `scope`, `cursor`, `limit`, her biri en çok bir kez; başka ad
   (harf farkı dahil — `Scope`), `from`/`to`, yinelenen ad ve ayrıştırılamayan sorgu dizgesi (`%zz`, `;`) **400
   `invalid_request`** (§8'de parametre 400'dür, gövde alanı 422). Gerekçe: yok sayılan bir `curser` okuyucuyu akışın başına
   döndürür, yok sayılan bir `scop` öteki kapsamın punch'larını verir. `r.URL.Query()` bozuk çifti sessizce attığı için
   `url.ParseQuery`'nin hatası okunur. Ret istenen değeri yankılamaz.
3. **İmleç `cur_<seq>` — yalnız konum** (brief'in önerisi): tenant anahtardan, kapsam parametreden, her istekte. Biçim katı
   (önek, işaretsiz, baştaki sıfırsız ondalık, int64). Başka tenant'ın imleci bu akışta bir **konumdur**: akışın ötesindeyse
   400, içindeyse bu tenant'ın o konumdan sonraki olayları — öteki tenant'a dair tek bir bayt vermez (söylediği, okuyanın
   kendi akışının uzunluğudur). İmlecin imzası yoktur: taşıdığı tek şey okuyanın kendi akışında bir konum; sahte bir imleç bir
   konum seçmekten öte bir şey kazandırmaz. **Akışın ötesindeki imleç 400** (`tenant.ErrCursorAhead`): sayacı ileri atma
   adımını atlayan bir geri yükleme (§5) entegratörü sayaç onun imlecini geçene kadar sessizce boş sayfada bekletirdi; şimdi
   yüksek sesle durur. Geri yükleme adımıyla açılan boşluktaki konumlar geçerlidir.
4. **Yanıt `{data, nextCursor, hasMore}`; `nextCursor` her zaman dolu** — boş sayfada okuyucunun yetiştiği konum: isteğinki
   ya da (imleçten sonra olay yoksa) sayacın değeri; hiç geri gitmez. `hasMore`: sayfa okunduğu anda akışta bu sayfadan
   fazlası vardı. Employees'in "dolu sayfa → `nextCursor`" kuralı burada kullanılmadı: bir akışı yoklayan okuyucunun tek bir
   değer saklaması gerekir, ve 6. maddenin boş sayfası "dolu sayfa" sezgisini bozar.
5. **Okumanın ispatı — iki ifade, sıra ispattır** (§5'in okuma yarısı). Önce `GetPunchFeedHead`: sayaç `S` ve imleçten
   sonraki ilk olay; sonra `ListPunchEvents`: `(after, upto]`, `upto ≤ S`, `seq` sırasıyla, `limit + 1` satır. Sayaç satırı
   artıştan commit'e kadar kilitli olduğu için anlık görüntünün gördüğü olaylar tam olarak `1..S`'dir ve `≤ S` her olay
   ilk ifade başlamadan commit edilmiştir — READ COMMITTED'da daha sonraki ikinci ifade hepsini görür. Kısa sayfa pencerenin
   tamamını okumuştur; pencerenin sonu atlamayan bir imleçtir. Kesilen sayfanın imleci verilen son olaydır.
6. **Pencere** (`tenant.PunchFeedWindow` = 10 000 konum). Kapsam bir rol süzgecidir; akışı çoğunlukla öteki kapsamın
   satırları olan bir tenant (çalışanları eşin plaketlerinde çalışan işveren, `scope=location`) sayfayı doldurmak için
   sınırsız yürürdü. Pencere imleçten sonraki **ilk olaydan** başlar (geri yükleme boşluğu ya da uzun bir aralık tek adımda
   geçilir) ve `S`'de biter. **Bedel (adlandırıldı, OpenAPI'de ve plan §5 API-12'de):** `limit`'ten az, hatta boş bir sayfa
   `hasMore: true` ile gelebilir; 1 M öteki-kapsam satırı 100 boş sayfadır.
7. **Alanlar:** `pch_<işverenin kayıt id'si>` (holding punch'ının iki satırında aynı), `version`, `status`, `punchTime`
   (`occurred_at`), `recordedAt` (olayın `recorded_at`'i — onaylı flag'de onay anı), `channel`, `direction`, `employee {id,
   externalRef, employeeCode, company}`, `location {id, externalRef, name, company}` — her zaman nesne, alanları null
   olabilir (lokasyonsuz plaket; eşlik bitmişken onay — API-2 sapma 4), `tagId` (`tag_` + uid; `character(14)`'ün dolgusu
   kırpılır). Zamanlar UTC. `roles` ve `seq` yanıtta yoktur (kapsam istekten, konum imleçten). Yanıt alan alan kurulur.
8. **Askı:** okuma kontrol noktasından geçmez — askıda açık (§10). K-22 (403) ve iptal (401) zincirde.

**Ölçümler.**
- **Dört şekil × iki kapsam** — `TestPunches_FourShapesTimesTwoScopes`: KF@KF, KM@KM, KM@KF, KF@KM; her işletmenin her
  kapsamı tam beklenen kümeyi sırayla verir ve her yanıt kendi akış satırına alan alan eşittir; holding punch'ının iki yanıtı
  aynı `id`, zaman ve yönü taşır; KM@KF'de KF'nin lokasyonu (şirket KF), KF@KM'de KM'nin. C ve D (H2) hiçbirini görmez
  (`TestPunches_OutsiderSeesNone` — KF'nin her konumu C'nin elinde: yalnız C'nin punch'ı ya da 400). Eşlik bitmişken onay →
  KM'de bir kez, lokasyon null, `tagId` KF'nin plaketi; KF'de yok (`TestPunches_ApprovalAfterTheHoldingEndedNamesNoCounterpart`).
- **K-4** — `TestPunches_NoManualNoRejected`: manuel, practice (ok ve onaylı flag), reject, ignored, reddedilmiş, bekleyen ve
  onaylı manuel flag hiçbir kapsamda yok; onaylı flag bir kez, onay anında, akış sırasındaki yerinde; KM'nin KF plaketindeki
  onaylı flag'i KM `employer` ve KF `location`'da birer kez; holding manuel ve reddi yok.
- **Eşzamanlı okuyucu** — `TestPunches_PagesNeitherSkipNorRepeatUnderConcurrentWriters` (API-2 stresi okuma ucundan): 12 yazıcı
  × 40 satır, dört işletme, iki yönlü holding dokunuşları, onda bir tümden geri alma, onda bir savepoint geri alımı; işletme ve
  kapsam başına bir okuyucu uçtan 1–7'lik sayfalarla. Koşu: 864 olay, 651 sayfa, **0 kaçan, 0 yinelenen**. **Mutantlar
  (her biri 3/3 kırmızı):** sayaç sayfadan **sonra** okununca (sonraki anlık görüntü) kaçan 3–11 / okuyucu; `upto` sınırı
  kaldırılınca yinelenen 3–11 / okuyucu.
- **Diğer mutantlar** (her biri kırmızı): kesilen sayfanın imleci sayaç → `Limit1001Is400`, `CursorGames`, stres; rol süzgeci
  sabit `employer` → dört şekil, K-4, stres; lokasyon şirketi çalışanınkinden → üç test; akışın ötesindeki imleç kabul →
  `CursorGames`; önek denetimi gevşetilince (`"1"`) → `CursorGames`; bilinmeyen parametre yok sayılınca ve kapsam varsayılan
  `employer` olunca → `TheQueryIsTheContract`; bozuk sorgu `r.URL.Query()`'ye düşünce → `TheQueryIsTheContract`; sayfa
  sorgusunda `tenant_id` yüklemi `OR true` ile etkisizleşince → kemer (`TestStaffQueries_CarryAnExplicitTenantPredicate`)
  kırmızı, HTTP izolasyon testleri RLS sayesinde yeşil (kuşak + kemer, API-5 emsali); baş sorgusunun alt sorgusunda yüklem
  kaldırılınca → kemer kırmızı; `api.New`'e `Punches` verilmeyince → kablolama testi ve uçtan uca test kırmızı.
  *Adlandırılmış (yeşil kalan, kusur değil):* yetişmiş okuyucunun imleci sayaç yerine isteğin imleci olunca dört HTTP
  testi yeşil kaldı — atlama ya da yineleme doğurmaz, yalnız ilerlemez; `TestPunchFeed_TheWindowBoundsASparseScopeAndAGapIsCrossed`
  bunu pinler (geri yükleme boşluğunda okuyucu yeni sayaçta yetişmiş sayılır).
- **Kanarya** — `TestPunches_NothingButTheFeedsColumnsAnswers`: arkadaki her satırın notu, kaynak adresi, koordinatı,
  kuralı, policy bağlamı ve güveni, her çalışanın e-postası kanarya taşır; iki işletmenin iki kapsamının yanıt baytlarında
  hiçbiri ve `verdict`/`trust`/`note`/`email`/`gps`/`policy`/`sid`/`roles` sözcükleri yok; her punch tam sözleşme anahtarlarını
  taşır.
- **Ölçek** (atılabilir kap, Docker Desktop macOS; iki tenant, her birinde 1 001 000 olay — 10 000'i gerçek tetikleyicilerden,
  990 000'i aynı biçimde toplu yazıldı (yalnız laboratuvar), 1 000'i gerçek tetikleyicilerden; `ANALYZE`'lı): `EXPLAIN
  (ANALYZE)` — baş sorgusu `punch_events_pkey` üzerinde tek Index Only Scan (0,2 ms); sayfa `punch_events_pkey` Index Scan,
  `(tenant_id, seq)` aralığı + rol süzgeci, Sort yok: 1 000 satır 1,0 ms, boş 10 000'lik pencere 7 ms (soğuk). Okuyucu
  üzerinden (`PunchPage`, 20 tekrar): `limit` 500 medyan 4,7–5,6 ms, 1 000 medyan 6,8 ms (başta, ortada, sonda aynı); seyrek
  kapsam (1 M `employer` satırının arkasında 1 000 `location`): 101 sayfa (100 boş), toplam 1,37 sn, en kötü sayfa 273 ms
  (soğuk); yoğun 1 M yürüyüş 1 001 sayfa, toplam 6,7 sn, en kötü 50 ms. Süre plana bağlı olduğu için pinlenmez.

**Devredilen / açık — orkestratöre:** **Geçmiş sorgusu** (`from`/`to`, ≤ 31 gün — §5 son cümlesi, plan §2 madde 18,
`punch_events_tenant_time_idx`): API-7 brief'inin kapsamında değildi ve plan §5'te sahibi olan bir görev yok. Bugün
`from`/`to` 400'dür (2. madde); eklenmesi geriye uyumludur. Kendi imleci (`punch_time` sırası) ve kendi ölçümü ister.
