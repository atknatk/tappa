# M10 — Platform: acil güvenlik, operatör kimliği, e-posta (SES), white-label

> **Öncelik:** kullanıcı kararı (2026-09-24) — bu dosyadaki işler M9'un geri kalanından ve yeni
> özelliklerden ÖNCE çözülür (*"bu tarz problemler önce çözülmeli"*).
> **Kaynak:** üç bağımsız mimar ajanı (salt-okuma, HEAD `a57d6e7`) + orkestratör sentezi. Satır
> numaraları o commit'e aittir; uygulama anında yeniden doğrulanır.
> **Durum işareti bu dosyada tutulmaz** — canlı durum `state.md`. Kararlar §6'da: ⏳ kullanıcı
> onayı bekliyor, ✅ önerisiyle uygulanır (otonomi kuralı; itiraz edilirse değişir).

## 0. Faz 0 — Acil güvenlik (tasarımdan bağımsız, hemen)

### Olay A-0 (2026-09-24'te tespit)
Operatör hesabının (allow-list'teki tek id, Kebab Factory Ltd. owner'ı) canlı panel parolası,
bir "rotate edilmeli" borç notunun İÇİNDE `state.md`'ye yazılmış (4 satır, 3 commit,
2026-09-19'dan beri pushlanmış). **Depo `atknatk/tappa` PUBLIC.** Hesap `/admin/legal` üzerinden
Taptime'ın herkese açık gizlilik/künye metinlerini değiştirebilir ve tenant verisini okur.
- **Triage (2026-09-24, salt-okuma):** hesabın 9 oturumunun ve 9 `admin.login.succeeded`
  satırının hepsi bilinen etkinliğe (19 Eylül kurulum otomasyonu + kullanıcının kendi cihazları)
  karşılık geliyor · 20 Eylül 08:44'teki tek istek MEVCUT bir oturum çereziyle `GET /admin`
  (parolayla yeni oturum değil) · başarısız giriş 0 (pod logu 2026-09-20 08:25'ten beri) ·
  19 Eylül'den beri tek yasal yayın bizim künye · audit'teki her eylem kullanıcının kurulumu.
  **Yetkisiz kullanım kanıtı yok.**
- **Yapılan:** değer HEAD'den kaldırıldı (`920baaa`). DB parolaları ve eski panel parolası
  repoda/geçmişte YOK (ölçüldü) — ama sohbette açığa çıktılar, rotate borcu sürüyor.
- **Kök neden:** sırrın DEĞERİ bir kayıt dosyasına yazıldı. Kural: sırra yalnız adıyla atıf.

| ID | Görev | Kim | Kabul |
|---|---|---|---|
| F0-1 | Operatör panel parolasını değiştir (Account → Change password, T73). Yeni parola sohbete/repoya yazılmaz | **Kullanıcı** | eski parolayla giriş 401; diğer oturumlar iptal (K3) |
| F0-2 | Scrub commit'ini push et | orkestratör (kullanıcı onayıyla) | `origin/main`'de değer 0 |
| F0-3 | Depo görünürlüğü kararı (öneri **private**) — geçmişte değer kalıyor; rotate sonrası ölü ama depo operasyon ayrıntısı taşıyor | **Kullanıcı** | karar private ise `gh repo view` → PRIVATE |
| F0-4 | `tappa_app` + `tappa_owner` DB parolalarını rotate et (tappa-secrets + `ALTER ROLE`) | **Kullanıcı** (§4.7: ajan tappa-secrets'a dokunmaz) | `/readyz` 200; eski parolayla bağlantı reddi |
| F0-5 | Sır sızıntısı kapısı: `redline-check.sh`'e kural — commit'lenen her dosyada kimlik bilgisi biçimleri (`postgres://…:…@`, `AKIA…`, `$2a$..$` digest, uzun hex parola biçimleri, `password=`) → FAIL; + agent-brief sabit kurallarına "sır DEĞERİ hiçbir dosyaya yazılmaz" | builder + güvenlik | mutasyon: state.md'ye sahte `postgres://u:p@h/db` eklemek redline'ı kırmızıya çevirir |
| OP-2 | Allow-list parser birim testleri (A-5) | builder | boş/boşluk→nil; `a,,b`→hata; e-posta→hata; nil uuid→hata; tekrar→tek; nil-reddi mutasyonu kırmızı |
| OP-3 | `tenants` UPDATE yetkisini daralt (A-4): `REVOKE UPDATE` + `GRANT UPDATE (name, business_type, timezone)` | tappa-db-migrator + güvenlik | `has_column_privilege(tappa_app,'tenants','vat_number'\|'structure','UPDATE')=false`; kapalı-liste testi genişletildi; signup E2E yeşil; Down temiz |

## 1. Kullanıcının endişesinin denetimi — hüküm
*"Her restoran admini sistem verisini güncelleyebiliyor mu?"* → **Kodda yetki açığı YOK.** 24 panel
POST rotasının tamamı tek tek denetlendi:
- Yasal metinler (Taptime künyesi dahil — kullanıcının gördüğü *"company details"* bu sekmenin
  açıklaması, `adminview.go:354-365`) yalnız allow-list'teki tek hesaba açık; diğer herkes GET ve
  POST 403 (`TestOperatorGate_TheServerRefusesTheRouteItself`, `…EmptyAllowListAdmitsNobody`,
  `…IsNotJoinableByRegisteringABusiness`).
- `sys:*` guardrail'ler kodda, DB'de satırı yok (`TestPolicies_LayerCheckRejectsGuardrail`); plan/
  fiyat/VAT doğrulama sütunları `tappa_app`'e kapalı; başka tenant FORCE RLS + açık filtre ile
  görünmez (`TestRLS_ReadIsolation_AllTables`, `TestRLS_AppRoleHasNoBypass`).
- Account'taki ad/tür/saat dilimi tenant'ın KENDİ satırı — meşru.

**Asıl sorun yapısal:** platform gücü bir restoran hesabına bağlı (allow-list kimliği = KF owner'ı)
→ o hesabın parolası sızınca (A-0) platform açığı oluyor; MFA yok, sunucu tarafı oturum süresi yok
(`adminauth/cookie.go:44-73`), operatör audit'i restoranın tenant'ına yazılıyor. Akış A çözer.
Küçük bulgular: A-2 encode global uid işgali (ADR 0017 md.12, bilinen) → OP-18 · A-3 signup'ta VAT
işgali → OP-11/15/16 · A-4 → OP-3 · A-5 → OP-2 · A-7 encode step handle admin'e bağlı değil → OP-18.

## 2. Sıralama (öneri — ⏳ D-D)
```
Faz 0 (bugün) ─► A1 Operatör kimliği + legal taşıma (OP-4..OP-10)
                 ║ paralel: kullanıcının SES dış adımları (AWS / DNS / sandbox çıkışı — günler sürer)
              ─► B  E-posta: taşıyıcı → reset → davet (EM-1..EM-8)
              ─► C  White-label (WL-0..WL-12)
              ─► A2 Tenant-ötesi okuma/yazma: liste, fatura, askı, VAT, plaket (OP-11..OP-18)
```
Gerekçe: (1) A-0 tam olarak "platform gücü restoran hesabında" sınıfının sonucu — önce o kapanır;
(2) SES'in dış adımları gün alır, erken başlamalı; reset e-postası owner'ın hesap kurtarması (bugün
DB'siz yol yok); (3) white-label ürün cilası ve §9 kararına bağlı; (4) A2 çoklu-müşteri operasyon
araçları — pilot büyüdükçe. Akışlar paralel ÇALIŞTIRILMAZ (paylaşılan Postgres, sıralı denetçi) —
kullanıcının dış adımları hariç.

**Numaralandırma:** her ADR/migration yazıldığı anda sıradaki boş numarayı alır (bugün ADR 0020,
migration 00024). Önerilen ADR sırası: 0020 operatör kimliği · 0021 `op_*` arayüzü · 0022 e-posta
taşıyıcısı · 0023 tenant markası/§9 · 0024 kullanıcı yüklediği görsel · 0025 tenant askıya alma.

**Akışlar arası çakışma çözümleri:**
- **E-posta gönderen adı:** faz 1'de SABİT `Taptime <no-reply@taptime.mt>` (EM-K5). WL planının
  "X via Taptime" önerisi ertelendi — tenant adı herkese açık signup'la saldırgan kontrolünde →
  oltalama kaldıracı; ancak VIES-doğrulanmış tenant koşuluyla, WL-11 kapsamında.
- **Panel kabuğu:** OP-10 (TabLegal/Operator kaldırma) WL-8'den (logo/şerit) ÖNCE.
- **Askı (OP-15):** `ProtectWriting` içindeki `suspensionGate` WL marka rotalarını ve EM davet
  rotasını otomatik kapsar; OP-15 kabulü router'dan türetilen rota listesiyle olduğu için yeni
  rotalar kendiliğinden teste girer.

## 3. Akış A — Platform operatörü (süper admin)

### Öneri: (c) hibrit — ⏳ D-B
Ayrı `platform_admins` kimliği (ayrı tablo, çerez, oturum, zorunlu TOTP, sunucu tarafı oturum
süresi), `/operator` ağacı tercihen `ops.taptime.mt`; tenant-ötesi erişim YALNIZ ayrı bir LOGIN
rolünün (`tappa_operator`, NOBYPASSRLS) çağırabildiği, adı konmuş `SECURITY DEFINER op_*`
fonksiyonlarıyla; tek binary, tek deployment. **Önceki "B — allow-list'i genişlet" kararının yerine
geçer.** Gerekçe: M9-08'in 4. kabul kriteri (*"müşteri oturumu /operator'a hiçbir koşulda
giremez"*) (a) ile yapısal olarak karşılanamaz (operatör oturumu = müşteri oturumu) · ADR 0016 §2
tam olarak (a)'nın gerektirdiği deseni (*"müşteri kimliğiyle ulaşılan handler'da
`WithTenant(başkası)`"*) yasaklıyor · A-0 · ADR 0016 §5'in "yeni giriş gereksiz" gerekçesi dört
yasal belge içindi, M9-08'in kapsamı §4.5'i aşan beş işlev.

| Kriter | (a) allow-list | (b) ayrı süreç/deploy | **(c) hibrit** |
|---|---|---|---|
| Müşteri oturumu → operatör yüzeyi | aynı çerez, handler başına kapı | yapısal olarak imkânsız | yapısal olarak imkânsız |
| MFA / oturum süresi | yok | var | var |
| Tenant-ötesi DB | `WithTenant(başkası)` ya da bypass | rol + definer | rol + definer |
| Signup'tan sömürü | her tenant sahibi potansiyel saldırgan | yok | yok |
| Aynı süreçte RCE | her şey | operatör DSN'ine ulaşılamaz | ulaşılır (K9 ile kapanır) |
| Efor | düşük başlar, her ekranla artan risk | en yüksek | orta–yüksek |

### Tasarım özü (ADR 0020/0021'de normatif olacak)
- **Kimlik:** `platform_admins` — email citext GLOBAL UNIQUE, bcrypt cost 12, parola ≥14 rune,
  `totp_secret_sealed` (AES-256-GCM, ayrı KEK `TAPPA_OPERATOR_TOTP_KEK`), `totp_last_step` (tekrar
  koruması §4.4'ün aynası: `UPDATE … WHERE totp_last_step < $2`, 0 satır → ret), status
  pending/active/disabled, CHECK active⇒parola+TOTP. `platform_sessions` — mutlak 8 saat, boşta
  30 dk, `mfa_verified_at`, `revoked_at`; touch tek ifade, fail-closed. Çerez `__Host-taptime_op`
  (Secure, HttpOnly, SameSite=Strict, Domain yok), ayrı HMAC etiketi, farklı redaction placeholder.
- **Giriş:** e-posta+parola (bilinmeyen e-postada sabit bcrypt) → kısa ömürlü imzalı ara çerez →
  TOTP → oturum. TOTP stdlib (RFC 6238, HMAC-SHA1, 6 hane, 30 sn, ±1 adım); QR kütüphanesi yok —
  base32 + `otpauth://` elle. flood/attempt/account limiter + N hatalı TOTP'ta kilit + audit.
  Kurtarma kodu yok; cihaz kaybında `opadmin reset-mfa`.
- **Rota/host:** `/operator/login`, `/operator/login/totp`, `/operator/enroll`, `/operator/logout`,
  `/operator` (tenant listesi), `/operator/tenants/{id}`, `/operator/legal`, `/operator/billing`,
  `/operator/plaques`, `/operator/audit`. Zincir: hostGate → floodGate → sameOriginGate (operatör
  origin'i) → requireOperator → sessionGate. Oturumsuz → 303 login; bilinmeyen rota 404; DSN yoksa
  503 + adı konmuş fault. `TAPPA_OPERATOR_HOST` host kapısı (`taptime.mt/operator` 404). ops ve ana
  host AYNI SITE → SameSite yetmez, `sameOriginGate` şart (T38/M8-04 dersleri).
- **DB (§4.5 bilerek aşılıyor — tek yer `op_*`):** `tappa_operator` (LOGIN, NOBYPASSRLS, yalnız
  `op_*` EXECUTE + `platform_*` dar sütun yetkileri, `platform_admins` INSERT YOK) ·
  `tappa_opdefiner` (NOLOGIN, BYPASSRLS, `op_*` sahibi, SÜTUN düzeyi grant — `aes_key_ref`,
  `app_key_ref`, `password_hash`, `token_hash`, `code_hash` ASLA). `tappa_app` hiçbir yeni yetki
  almaz; `platform_*` üzerinde açık `REVOKE ALL` (T46: prod default privileges geniş); `op_*`
  üzerinde EXECUTE yok (`REVOKE ALL … FROM PUBLIC`). Elenenler: BYPASSRLS havuzu (`pool.go:303-311`
  prod'da reddeder, tek eksik `WHERE` her şeyi açar) · operatörün `WithTenant` döngüsü (liste
  çıkarılamaz, yasak deseni normalleştirir). Roller `scripts/db-init/01-roles.sql` + canlı küme
  için tek seferlik runbook (R5b migration'daki BYPASSRLS'i yakalar; `01-roles.sql` tarama dışı).
- **`op_*` sözleşmesi:** (i) ilk parametre operatör oturum token hash'i — fonksiyon canlı+MFA'lı
  oturumu KENDİSİ çözer, aktörü türetir (beyan edilemez); (ii) sabit sütun listesi, `SELECT *` yok;
  (iii) LIMIT ≤200; (iv) `SET search_path = pg_catalog, public`, dinamik SQL yok; (v) her çağrı
  `operator_audit_log`'a, değiştirenler ayrıca hedef tenant'ın `audit_log`'una AYNI transaction'da;
  (vi) hepsi tek migration + `db/queries/operator.sql` (belge) + elle yazılmış
  `internal/db/operator.go` (`resolve.go` emsali). Go: `db.OperatorDB` — `WithTenant` metodu YOK;
  müşteri paneli operatör paketini import etmez (testle).
- **Audit:** `operator_audit_log` (tenant_id YOK — ADR 0016 benzeri muafiyet, R5 WARN; append-only:
  REVOKE + `tappa_forbid_mutation` trigger) HER eylemi tutar — başarılı/başarısız girişler,
  enrollment, yayınlar ve tenant verisinin OKUNMASI (liste + detay; "sessiz tenant-ötesi okuma
  yok"). Tenant'ı değiştirenler tenant'ın `audit_log`'una da (`actor_id` = platform admin id,
  polimorfik/FK'sız — migration gerekmez; `detail.actor_kind="operator"`). Detail'e TOTP sırrı,
  token, parola ASLA.
- **Askı (ADR 0025):** `tenants.suspended_at`, `suspension_reason` (tappa_app SELECT evet, UPDATE
  hayır). Bloke: `ProtectWriting` altındaki bütün yazmalar (encode dahil) → 403 + gerekçe + ret
  audit'i; istisna parola değiştirme + çıkış. Bloke ETMEZ: NFC/QR tap'ler (§4.6 — karar motoruna
  dokunulmaz; guardrail eklemek ADR 0004 değişikliği ister), okumalar, CSV (GDPR taşınabilirlik +
  işverenin yasal kayıt yükümlülüğü), giriş, aktivasyon. Sıcak yol: `TouchAdminSession`
  `suspended_at`'i döndürür → `AdminIdentity.Suspended` → requireAdmin sonrası `suspensionGate`,
  ek sorgu yok. Sert kilit ayrı eylem (`op_disable_tenant_admins`).
- **Bootstrap:** `cmd/opadmin` filtre CLI (`cmd/rotatekek` emsali — DSN yok, sürücü yok, SQL üretir,
  operatör `psql` ile `tappa_owner` olarak uygular): `create --email --name` (pending satır +
  enrollment token hash'i; ham token stderr'e BİR KEZ, TTL 30 dk, SQL çıktısında ASLA) ·
  `reset-mfa` · `disable`. `platform_admins`'e yalnız `tappa_owner` INSERT → HTTP'den ya da app
  rolünden operatör yaratmak imkânsız; tablo boşsa kimse giremez. Reddedilen: env'den tohumlama,
  herkese açık kurulum sayfası.
- **Legal taşıma:** `/operator/legal` → `op_publish_legal(session, slug, body)`; `REVOKE INSERT ON
  legal_documents FROM tappa_app` (ADR 0016 §2b "yazma tarafında DB derinliği yok" borcu kapanır);
  `published_by` = `platform_admins.id` (eski satırlar "tenant admin (legacy)"); geri alma = yeni
  satır; tek replika → yayından sonra `legal.Store.Refresh`. Allow-list mekanizması tamamen
  kaldırılır: `TabLegal`, `OperatorOnly`, `PanelChrome.Operator`, `mayPublishLegal`,
  `config.OperatorAdminIDs`, `TAPPA_OPERATOR_ADMIN_IDS` (`05-config.yaml`, `.env.example`, README).
  KF hesabı sıradan müşteri olur.
- **Yapılmayacak:** müşteri adına giriş (impersonation). Gerekirse ileride tenant onaylı, süreli,
  salt-okuma destek erişimi (ayrı ADR).

### Görevler — A1 temel
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| OP-4 | ADR 0020 (operatör kimliği; ADR 0016 §5 + M7-06 "yeni giriş yok" + B kararının yerine geçer) + ADR 0021 (`op_*` arayüzü, elenenler) | S–M | orkestratör (+güvenlik okuması) | ADR'ler kabul; ADR 0016 durumu "kısmen yerine geçildi" | D-B |
| OP-5 | Migration: `platform_admins`, `platform_sessions`, `operator_audit_log` + `01-roles.sql` + runbook | M | tappa-db-migrator | tappa_app dört fiilde yetkisiz (prod default-priv simülasyonu dahil); tappa_operator `platform_admins` INSERT yok; `operator_audit_log` UPDATE/DELETE tappa_owner için bile hata; `RoleFacts.Privileged()=false`; active⇒parola+TOTP; R5 WARN'lar; Down temiz | OP-4 |
| OP-6 | `internal/operatorauth` (parola, TOTP, oturum, çerez, token, limiter/kilit) | L | builder + güvenlik | RFC 6238 Ek B vektörleri; aynı kod N goroutine → tam 1 başarı (-race); yanlış KEK açamaz; sızıntı testleri harici pakette; 8 s / 30 dk / MFA'sız / revoked / disabled hepsi red (saat enjekte DB testleri) | OP-5 |
| OP-7 | `OperatorDB` + config (`TAPPA_OPERATOR_DATABASE_URL`, `TAPPA_OPERATOR_TOTP_KEK`, `TAPPA_OPERATOR_HOST`) | M | builder | DSN yoksa /operator 503, panel etkilenmez; prod'da ayrıcalıklı rolle boot reddi; reflection: `WithTenant` yok; OperatorDB yalnız operatör handler'larına; TOTP KEK diğer anahtarlarla aynıysa başlangıç reddi | OP-5 |
| OP-8 | `/operator` handler'ları + UI (tappa-brand: restoran paneliyle karıştırılamayan "TAPTIME OPERATOR" kabuğu; tenant-ötesi her ekran girdiği tenant'ı başlıkta ADIYLA gösterir) | L | builder + tappa-brand + güvenlik | çapraz çerez: admin/çalışan çerezi operatör çerez adına konunca 303 (ve tersi); yanlış host 404; cross-origin POST'ta resolver çağrısı 0; bilinmeyen e-posta = yanlış parola (gövde + bcrypt sayısı); TOTP tekrarı red; N hatada kilit + audit; her giriş `operator_audit_log`'da; CSP/no-store/nosniff | OP-6, OP-7 |
| OP-9 | `cmd/opadmin` + README + (K2/K4) ops Ingress/DNS | M | builder (+kullanıcı ops) | sürücü yok, owner DSN adı kaynakta yok; çıktı tek transaction; ham token stdout'ta yok; süresi geçen token red | OP-5 |
| OP-10 | Legal'i taşı + allow-list'i emekliye ayır | M | builder + tappa-db-migrator + güvenlik | `has_table_privilege(tappa_app,'legal_documents','INSERT')=false`; yayın sonrası `/legal/privacy` yeni metin; `TestLegalPublicPath_WritesNothing` + `TestLegalReader_CannotReachTheDatabase` yeşil; sürüm listesi yayımlayanı gösterir; geri alma = yeni satır; `rg 'OperatorAdminIDs\|OperatorOnly\|TabLegal\|mayPublishLegal\|TAPPA_OPERATOR_ADMIN_IDS'` kod+deploy'da 0 (ADR geçmişi hariç); müşteri panel taramasında operatör öğesi yok | OP-8, OP-9 |

### Görevler — A2 tenant-ötesi okuma/yazma
| ID | Görev | Efor | Kabul (özet) |
|---|---|---|---|
| OP-11 | Tenant listesi/arama/detay (`op_list_tenants`, `op_tenant_detail`) | M | tappa_app EXECUTE yok; tappa_operator `tenants` doğrudan SELECT yok; süresi geçmiş/MFA'sız hash → exception; her çağrı tam 1 operatör audit satırı; anahtar/hash sütunları için `has_column_privilege(opdefiner,…)=false`; tenant başlıkta adıyla |
| OP-12 | Faturalama görünümü (salt-okuma) | M | tutarlar tenant önizlemesiyle aynı (fixture'da çapraz test); `numeric`, float değil |
| OP-13 | Plaket envanteri (salt-okuma) | S–M | anahtar sütunlarına erişim yok (katalog testi) |
| OP-14 | Operatör audit görüntüleyici | S | — |
| OP-15 | Askıya alma / yeniden etkinleştirme (ADR 0025) | L | router'dan türetilen HER yazma rotası 403 + audit; GET/CSV 200; askıdaki tenant'ın NFC/QR tap'i askısız ile AYNI karar + trust (tablo testi, §4.6); tappa_app `suspended_at` UPDATE edemiyor; iki audit satırı aynı tx (rollback testi); reinstate yazmaları açıyor |
| OP-16 | VAT yeniden doğrulama | M | tappa_app `vat_*` hâlâ UPDATE edemiyor; tenant audit önce/sonra; VIES kesintisinde yazma yok, cümle gösteriliyor |
| OP-17 | Plan/fiyat | M–L | **T27/T37 kararına bloke** |
| OP-18 | Operatör encode'u + tenant tarafı encode'u kapatma (ADR 0017 md.12 kapanır; A-2, A-7) | L | Android uygulaması operatör host'una giriş yapar; önce operatörün hedef tenant'a yazabilmesi |
| OP-19 | Süreç ayrımı (aynı imaj, ayrı Deployment) | — | K9: pilot sonrası yeniden bak |

## 4. Akış B — E-posta (AWS SES)

### Öneri: SES SMTP arayüzü + stdlib `net/smtp` (STARTTLS 587), `eu-central-1` — ✅ (sıfır yeni modül)
| | (a) SES SMTP + `net/smtp` | (b) SES v2 HTTP + elle SigV4 | (c) `aws-sdk-go-v2` |
|---|---|---|---|
| Yeni modül | **0** | **0** | servis başına ≥4; `config` ile +8-9 |
| Kripto | yok (TLS stdlib) | HMAC zinciri bizde — §3 "kripto yalnız `internal/sun`" gerilimi | SDK'da |
| Test/dev | aynı kod yolu sahte SMTP'de ve dev yakalayıcıda koşar | yalnız SES | yalnız SES |
| Bakım | `net/smtp` dondurulmuş ama Go güvenlik politikası kapsamında | imza + saat kayması (±5 dk) bizde | govulncheck yüzeyi, sık sürüm |

### Tasarım özü (ADR 0022'de normatif)
- **`internal/mail`** (yalnız stdlib; davet/reset kavramını bilmez): `Message{To, Subject, Text,
  HTML, Ref}`, `Receipt{MessageID}`, `SMTP.Send(ctx, Message)`. `DialContext` + deadline →
  STARTTLS **zorunlu** (ilan edilmezse hata, düz metne düşüş yok; TLS ≥1.2) → PlainAuth →
  Mail/Rcpt → DATA elle (250 yanıtından Message-ID) → Quit. 4xx için bellekte tek geri çekilmeli
  deneme; link kalıcı bir kuyruğa YAZILMAZ.
- 🔴 **Sızıntı kuralı:** `SendError{Class, SMTPCode}` — `Error()` sunucu metni ya da adres
  İÇERMEZ, `textproto.Error`'ı SARMAZ (Unwrap yok). Ölçülen gerekçe: davet hata yolu bugün hata
  zincirinin tamamını logluyor (`employeeactions.go:419`); SES sandbox reddi alıcı adresini metinde
  taşır → log'a düşerdi. Sınıflar: invalid_address · tls_unavailable · auth · rejected · throttled ·
  network · timeout.
- Başlık üreticisi CR/LF/kontrol karakteri içeren değeri REDDEDER (temizlemez); alıcı
  `mail.ParseAddress` birebir (Name boş, Address = girdi), tek, ASCII, ≤254. SMTP parolası redakte
  eden tipte (`invite.Code` deseni: Format/String/GoString/LogValue).
- **Config (akış başına):** `TAPPA_RESET_DELIVERY` none|email · `TAPPA_INVITE_DELIVERY`
  panel|email · `TAPPA_SMTP_HOST` (prod'da localhost/IP literal yasak) · `TAPPA_SMTP_PORT` (587;
  465 red — STARTTLS tasarımı, Hetzner engeli) · `TAPPA_SMTP_USERNAME` / `TAPPA_SMTP_PASSWORD`
  (**Secret**, `optional: true`) · `TAPPA_MAIL_FROM` (`Taptime <no-reply@taptime.mt>`) ·
  `TAPPA_MAIL_REPLY_TO` (ops.). Bir akış `email` iken SMTP ayarı eksikse boot hatası (fail-closed).
  `TestPackaging_EverySecretConfigReadsIsInjectedByTheManifest` yeni env'leri manifestte zorlar.
  R7 tuzağı: config hata mesajında değişken adı `%s` ile basılır.
- 🔴 **Reset gönderimi istek yolundan ÇIKAR (zorunlu):** senkron SES (TLS + AUTH + DATA) 250 ms
  tabanını aşar → kayıtlı/kayıtsız adres zamanlamayla ayırt edilir (`adminresetlimits.go:308-316`
  bunu öngörüyor). Token'lar senkron basılır; grant'lar sınırlı süreç-içi işçiye (tampon ~32, 1-2
  goroutine, `context.WithoutCancel` + 15 s), işçi mevcut `h.deliver`'ı çağırır (gönder-sonra-tek-
  audit korunur); kuyruk doluysa senkron `undelivered`; kapanışta ≤3 s boşaltma
  (`shutdownbudget_test.go`). Artık risk (gönderim anında çökme → audit'siz reset) ADR'ye.
- **Davet:** email modunda `IssueAndDeliver`'dan ÖNCE adres okunur (yeni sqlc sorgusu, tenant + id,
  işletme adıyla); yok / geçersiz / **aynı tenant'taki bir yöneticinin adresine eşit** (citext) →
  kod basılmaz, net cümle + audit. Ekranda "`<adres>` adresine gönderildi, 7 gün geçerli".
  "E-postayı değiştir" aksiyonu bekleyen davetleri aynı tx'te iptal eder
  (`CancelPendingInvitesForEmployee`). **Y-D (ADR 0005) kapanmaz, DARALIR** — müdür adresi kendisi
  yazıyor; plus-adresleme/alias artık risk; ADR 0005'e append.
- **Şablonlar:** yalnız İngilizce; UTF-8 + RFC 2047 konu (Maltaca ċ ġ ħ ż); HTML satır içi stil,
  tappa-brand paleti, metin wordmark "Taptime" — görsel / uzak font / dış URL / izleme pikseli YOK,
  tek mutlak URL eylem linki (testle); düz metin kısmı (link kopyalanabilir); sabit konu;
  tenant/çalışan adı yalnız gövdede, escape'li. Davet metni: "telefonun ana tarayıcısında açın"
  (iOS Gmail/Outlook uygulama-içi tarayıcısı çerezi Safari ile paylaşmaz; NFC varsayılan tarayıcıyı
  açar — state.md'de ölçülmüş tuzak).
- **Oran sınırları (herkese açık signup = spam vektörü):** tenant 50/saat + 300/gün, çalışan
  3/saat, süreç geneli ~300/saat devre kesici (sayılar uygulamada aritmetikle savunulur).
- **Log:** yalnız tenant_id, employee_id/admin_user_id, message_id, class, smtp_code — adres,
  link, sunucu metni ASLA.
- **SES:** yapılandırma seti TLS=Require, açılma/tıklama izleme KAPALI (tıklama izleme linki AWS
  yönlendiricisinden geçirir = sırrın üçüncü tarafa ifşası), hesap düzeyi bastırma listesi açık.
  IAM yalnız `ses:SendRawEmail`, kaynak identity + config set ARN, koşul `ses:FromAddress`.
- **"Shown once":** `panel` modu kalır; adresi olmayan çalışan için "linki göster" yedeği yalnız
  owner'a, `manager_panel` kanalı olarak audit'lenir (M6-11'de iki kanal ayrı görünür).

### Görevler
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| EM-1 | ADR 0022 + Q02 "Cevaplananlar"a + ADR 0005 Y-D append ("daraldı, kapanmadı") + ADR 0015 durum notu + M7-04/M7-07 notları | S | orkestratör | ADR'de (b)/(c) ölçüleriyle elenmiş; Y-D "kalkar" değil "daralır" | — |
| EM-2 | `internal/mail` SMTP taşıyıcısı | M–L | builder + güvenlik | test içi sahte SMTP (öz-imzalı): STARTTLS ilan edilmezse ClassTLS + sunucu 0 AUTH görür; 250'den Message-ID; takılan sunucuda deadline ≤100 ms; To/Subject'e `\r\n` → 0 dial ile hata; 30 s fuzz panik/CRLF sızıntısı yok; sunucu `550 … <adres> … ?t=TOKEN` dönerse `Error()`/`%+v`/slog JSON'da adres de token da YOK; parola her biçimde redakte | EM-1 |
| EM-3 | Config + manifest (`05-config.yaml`, `20-app.yaml` iki opsiyonel `secretKeyRef`) + runbook | S–M | builder | fail-closed matris tablo testi (her eksik anahtar ayrı vaka); packaging testi yeşil; prod'da localhost/465 red; ConfigMap hâlâ none/panel (davranış değişmez) | EM-1 |
| EM-4 | Şablonlar (`web/templates/email/*.templ` + metin üreticileri) | M | builder + tappa-brand | tam 1 mutlak URL; `<img`/`@font-face`/`<link`/`<script` 0; `<script>`'li ad escape; Maltaca doğru; konu `=?utf-8?q?`; kontrast AA (hesaplanmış) | EM-1 |
| EM-5 | Asenkron reset teslimi + `emailResetChannel` + main `case email` | M–L | builder + güvenlik | kanal gecikmesi 2 s iken kayıtlı/kayıtsız medyanları [taban, taban+50 ms] (yeni TimingIsFlat; senkron kodda kırmızı olduğu gösterilir); grant başına tam 1 audit; kuyruk dolu → senkron undelivered; kapanış bütçesi yeşil; M7-04'ün sahteye karşı koşmuş kriterleri GERÇEK SMTP'ye karşı yeniden koşulmuş (tablo rapora); canlı duman: Gmail "Show original" SPF=PASS (mail.taptime.mt), DKIM=PASS, DMARC=PASS | EM-2,3,4 + kullanıcı dış adımları |
| EM-6 | Adres okuma + "e-postayı değiştir" aksiyonu | M | tappa-db-migrator + builder + tappa-brand | RLS izolasyon (A, B'nin adresini okuyamaz); değişiklik bekleyen davetleri aynı tx'te iptal; audit; log'da `.Email` yok (R7b); migration beklenmiyor (ölç) | EM-5 |
| EM-7 | `invite.EmailChannel` + `emailLinkSink` + davet anahtarı + limitler | M–L | builder + güvenlik + tappa-brand | email modunda gövdede `/activate?code=` 0; `invite.code_emailed` 1, `code_shown_to_manager` 0; adres yok/geçersiz/yönetici adresi → `employee_invites` satırı 0; N+1. davet oran sınırında red, satır basılmaz; SMTP hatası log'da yalnız class/code (`employeeactions.go:419` yolu sızıntı testiyle kapalı); yedek yalnız owner'a + `manager_panel` audit | EM-6 |
| EM-8 | M6-11 kanal ayrımı + gerçek cihaz turu | S + kullanıcı | builder + kullanıcı | Gmail Android/iOS, Outlook, Apple Mail'den aktivasyon → NFC tap'te oturum tanınıyor; sonuç tablosu state.md'de | EM-7 |
| EM-9 | "Parolanız değişti" bildirimi (linksiz, yalnız giriş sayfası adresi) | S | builder | — | EM-5 |
| EM-10 | Bounce/complaint (SNS → HTTPS, imza doğrulama) | L | — | pilot sonrası, kendi ADR'si | — |
| EM-11 | Signup e-posta doğrulaması (ADR 0013 c) | L | — | pilot sonrası, kendi ADR'si | — |
| EM-12 | M7-07 yönetici daveti (kilidi açılır) | — | — | kendi kartı ve ADR'si | EM-7 |

### Kullanıcının dış adımları (EM-2 ile paralel başlar; sıralı)
1. AWS hesabı: root için MFA, günlük kullanım için ayrı yönetici kullanıcı, fatura alarmı (~$5).
2. SES `eu-central-1` → Identities → Domain `taptime.mt`: Easy DKIM (RSA 2048); Custom MAIL FROM
   `mail.taptime.mt` (MX hatasında "Use default MAIL FROM").
3. Cloudflare DNS (`taptime.mt`): 3× CNAME `<token>._domainkey` → `<token>.dkim.amazonses.com`
   (**proxy KAPALI / DNS only**) · MX `mail` → `feedback-smtp.eu-central-1.amazonses.com` (10) ·
   TXT `mail` → `v=spf1 include:amazonses.com ~all` · TXT `_dmarc` →
   `v=DMARC1; p=none; rua=mailto:<rapor adresi>; adkim=r; aspf=r` (2–4 hafta temiz rapordan sonra
   `quarantine`, ardından `reject`). Apex'teki mevcut MX/SPF'e dokunma.
4. SES'te DKIM ve MAIL FROM "Successful" olana kadar bekle.
5. Yapılandırma seti `tappa-transactional`: TLS Require · açılma/tıklama izleme KAPALI · reputation
   metrics açık · kimliğe varsayılan set olarak ata · hesap düzeyi bastırma (BOUNCE+COMPLAINT) açık.
6. Sandbox testi için kendi adresini Verified identity olarak ekle.
7. Production access talebi: Transactional; `https://taptime.mt`; "işverenin davet ettiği
   çalışanlara tek kullanımlık aktivasyon linki ve panel yöneticilerine parola sıfırlama; düşük
   hacim, pazarlama yok"; bounce/complaint: hesap düzeyi bastırma + izleme.
8. IAM: SES → SMTP settings → Create SMTP credentials; oluşan IAM kullanıcısının politikasını daralt
   (yalnız `ses:SendRawEmail`; kaynak identity + config set ARN; `ses:FromAddress =
   no-reply@taptime.mt`; opsiyonel `aws:SourceIp`). SMTP kullanıcı/parolası BİR KEZ gösterilir →
   parola yöneticisine.
9. 🔴 SMTP kimliklerini sohbete, commit'e, ekran görüntüsüne YAPIŞTIRMA (A-0 dersi).
10. `tappa-secrets`'a `TAPPA_SMTP_USERNAME`, `TAPPA_SMTP_PASSWORD` ekle (`read -rs` + patch) —
    ConfigMap'teki `…_DELIVERY=email` değişikliğinin deploy'undan ÖNCE (yoksa yeni pod boot'u
    reddeder; `maxUnavailable: 0` eski pod'u tutar, deploy başarısız görünür).
11. CloudWatch alarmları: `Reputation.BounceRate > 0.02`, `Reputation.ComplaintRate > 0.0005`.
12. Gizlilik politikası + DPA alt işleyici listesine "Amazon Web Services EMEA SARL — SES,
    eu-central-1, işlemsel e-posta" (`/admin/legal` ya da OP-10 sonrası `/operator/legal`).
13. (Ops.) Cloudflare Email Routing: `support@`, `dmarc@` → kendi kutun.
14. Gerçek cihaz turu (EM-8).

## 5. Akış C — White-label (tenant markası)

### Öneri: tek vurgu rengi + bir logo, co-brand — ✅ (tap ekranı ⏳ D-C)
İstek (*"kendi logosu ve renkleri"*) CLAUDE.md §9'la iki yerde çelişiyor: "paletin dışına çıkma"
ve "tap ekranına özellik eklemek istiyorsan önce sor". Uzlaşma: tenant YALNIZ tanımlı slotları
boyar, durum renkleri asla değişmez. Tenant logosu önde, küçük "taptime · punchless" kalır (GDPR
metni Taptime'ı işleyen olarak adlandırıyor, plaket "taptime" basıyor).

### Tasarım özü (ADR 0023/0024'te normatif)
- **Renk:** tek accent, serbest hex, **WCAG kapısı** — saf `internal/brand/accent.go`:
  `ParseAccent` (kanonik büyük harf) · `OnColor` (ink ya da paper, yüksek kontrastlı olan) · `Edge`
  (porcelain'e karşı <3:1 → 2 px ink kenar, WCAG 1.4.11) · `Check` (en iyi metin kontrastı <4,5:1
  → RED) · `Suggest` (aynı ton, açıklığı düşürerek geçen en yakın renk). Red bandı bağıl parlaklık
  L ∈ (0,1790; 0,2368) — orada ne paper ne ink 4,5'e ulaşır (ör. #808080 ve #E0457B red; #FFC72C
  sarı ink metin + ink kenarla geçer; #DA291C kırmızı paper metinle 4,78 geçer). Aynı fonksiyon
  yazma VE okuma tarafında (netx deseni). Palet sabitleri `tailwind.config.js`'den türetilip
  testle kilitli (ikinci kopya yok).
- **Asla değişmeyen:** beş kaşe damgası + durum→renk eşlemesi · tomato = hata/yıkıcı · saffron =
  FLAGGED/geç · Notice · docket/perforasyon · `.docket-label` · panelin birincil/yıkıcı butonları ·
  focus halkası (ink) · **sonuç ekranında accent hiç yok** (renkler durumu anlatıyor).
- **Enjeksiyon — CSP DEĞİŞMEZ:** durumsuz `GET /brand/theme/{HEX}.css` → gövde yalnız
  `:root{--brand-accent:R G B;--brand-on-accent:…;--brand-edge:…}`; DB okumaz, kimlik doğrulamaz,
  tenant verisi taşımaz (kurucusuna havuz verilmez — `/healthz` gibi yapısal); yalnız kanonik ve
  kontrolden geçen hex 200, gerisi 404; `public, max-age=31536000, immutable`, nosniff.
  `style-src 'self'` zaten kapsıyor; `style=` / `<style>` / `'unsafe-inline'` YOK (bugün hiçbir
  politikada yok, landing turunda "0 `style=`" ölçülmüş). Tailwind'e `brand`, `on-brand`,
  `brand-edge` token'ları `rgb(var(--…) / <alpha-value>)`; `:root` varsayılanı tappa-green/paper →
  marka ayarlamamış tenant'ta computed style bugünküyle aynı.
- **Logo:** yalnız **PNG/JPEG** (SVG doğrudan açılınca script çalıştırır ve stdlib'de güvenli
  sanitizer yok; GIF animasyon; WebP `x/image` bağımlılığı). Saf `internal/brand/logo.go`
  `Normalize(io.Reader)`: gövde `MaxBytesReader` 1 MiB, parça ≤512 KiB · `http.DetectContentType`
  magic bytes (istemci Content-Type ve dosya adı yok sayılır, LOGLANMAZ) · format-özel
  `png.DecodeConfig`/`jpeg.DecodeConfig` ile **decode'dan ÖNCE** her kenar 16–2048 px ve ≤4 MP
  (decode-bomb; en kötü 16 MiB RGBA, 512 Mi pod içinde) · süreç geneli 1–2 slot semafor · uzun
  kenar 512 px'e elle box filtre (stdlib; `x/image/draw` bağımlılık olurdu) · PNG→PNG
  (BestCompression), JPEG→JPEG q85 **yeniden kodlama** → EXIF/XMP/ICC/metin chunk'ları ve IEND
  sonrası polyglot düşer — 🔴 **§4.2: telefon fotoğrafının EXIF GPS'i silinir (testle)** · çıktı
  >256 KiB → "logoyu sadeleştir" · `sha256(çıktı)` URL anahtarı (⚠️ "fingerprint" kelimesi R1
  tetikleyicisi — "digest"/"sha256" kullan).
- **Depolama: Postgres `bytea`** — pod `readOnlyRootFilesystem` + hiç volume yok, kümede obje
  deposu yok (`50-backup.yaml` ölçmüş), `pg_dump` yedeğine kendiliğinden girer; 256 KiB × 1000
  tenant ≈ 256 MB, TOAST satır dışında tutar. Yükleme `r.MultipartReader()` ile AKIŞ —
  `ParseMultipartForm` büyük parçayı geçici dosyaya yazmaya kalkıp salt-okunur FS'te patlar.
- **Veri:** ayrı `tenant_branding` tablosu (0..1 ilişki; `store.Tenant`'a bytea taşıma riski yok):
  `tenant_id` PK (tablo-kısıtı biçimi — R5b), `accent char(6)` CHECK `^[0-9A-F]{6}$`, `logo bytea`
  ≤262144, `logo_sha256`, `logo_mime IN ('image/png','image/jpeg')`, `logo_width/height` 1–512,
  `updated_at`, `updated_by` + `FOREIGN KEY (updated_by, tenant_id) REFERENCES admin_users (id,
  tenant_id)`, logo alanları hep-birlikte-dolu CHECK; ENABLE+FORCE RLS, NULLIF politikası; GRANT
  SELECT/INSERT/UPDATE, DELETE YOK (sıfırlama UPDATE … NULL). Değiştirilebilir (marka hukuki delil
  değil); geçmiş audit_log'da. sqlc: `GetTenantBrand` (logo byte'larını SEÇMEZ — testle),
  `GetTenantLogo(tenant_id, sha)`, `UpsertTenantAccent`, `UpsertTenantLogo`, `ClearTenantAccent`,
  `ClearTenantLogo` — hepsi açık `tenant_id` filtreli.
- **Servis:** `GET /admin/brand/logo/{sha}` (panel okuma zinciri) ve `GET /t/logo/{sha}` (tap
  grubu, canlı oturum şart) — admin çerezi `Path=/admin`, çalışan çerezi `Path=/` olduğu için tek
  ortak rota iki çerezi alamaz. Tenant YALNIZ oturumdan; başka tenant'ın logosu ve var olmayan hash
  **bayt-aynı 404** (kehanet yok). Başlıklar: saklanan mime, `private, max-age=31536000,
  immutable`, `ETag: "<sha>"`, nosniff, `Content-Security-Policy: default-src 'none'; sandbox`,
  `Cross-Origin-Resource-Policy: same-origin`, sabit `Content-Disposition: inline;
  filename="logo.png"`. Sayfa CSP'sine `img-src 'self'` YALNIZ `<img>` render edilen sayfaya
  (`tapCSPFor(hasLogo)` / `adminCSPFor(hasLogo)`, `landingCSPFor` emsali). Kimliksiz hash-rotası
  elendi (SECURITY DEFINER ile tenant'lar arası okuma = ADR 0002 md.7'ye yeni istisna + bütçesiz DB
  okuması).
- **Yüzeyler:** tap ekranı logo (sabit yükseklikli başlık yuvası) + accent tap butonunda (⏳ D-C)
  · sonuç ekranı yalnız logo · tur/practice logo (faz 2) · panel kabuğu logo + tenant adı + 4 px
  accent şeridi (birincil butonlar yeşil kalır) · Account → "Your brand" editör + gerçek bileşen
  önizlemesi (mevcut "What your staff read" deseni) · aktivasyon (Taptime + işveren adı, bugünkü
  gibi) / problem / landing / legal / signup / admin login / reset / operatör / AdminChoose =
  Taptime · CSV değişmez · e-posta faz 1 yalnız ad (sabit Taptime gönderen), logo faz 2 CID (≤32
  KiB varyant; uzak URL = takip pikseli, elendi). Plaket/oturum tenant uyuşmazlığında
  (`ErrForeignLocation`, `tap.go:387-395`) Taptime varsayılanı.
- **Yetki/audit:** owner-only (`mayEditAccount` yeniden kullanılır); `tenant.brand_updated` aynı
  tx `RecordTx`, detail'de sabit 6 anahtar (`field`, `before`, `after`, `bytes`, `width`,
  `height` — görsel byte'ı ve dosya adı YOK); reddedilen deneme `tenant.brand_update_refused`.
  **§4.6:** marka okuma hatası hiçbir sayfayı düşürmez → varsayılana düşer + log (`PendingBadge`
  deseni); marka yoksa ek `<link>`/`<img>`/`img-src` yok, HTML bayt-aynı.
- **§4.1:** logo bir kişinin fotoğrafı olabilir ama biyometrik işleme yapılmaz; yüz tespiti
  EKLENMEZ (kendisi biyometrik işleme olurdu). Form metni "a logo, not a photo of a person".
- **Kabul edilen risk:** marka taklidi (kendi kendine kayıt olan tenant başka işletmenin logosunu
  yükleyebilir; plaket uyuşmazlığı `sys:tenant-mismatch` ile yakalanır) → ADR 0005 append.

### Görevler
| ID | Görev | Efor | Ajan | Kabul (özet) | Bağımlılık |
|---|---|---|---|---|---|
| WL-0 | Kararlar (D-C + WL-K*) open-questions'a cevaplı + ADR 0023/0024 + tappa-brand skill'e "Tenant slotları" taslağı | S | orkestratör | kararlar kayıtlı; ADR'ler kabul | D-C |
| WL-1 | Migration `tenant_branding` + `db/queries/branding.sql` + RLS testi | M | tappa-db-migrator | `make audit` R5/R5b 0; beşli tam; RLS testi `WHERE`'siz A bağlamında B'yi 0 görür + B `tenant_id`'li INSERT WITH CHECK ile red; `has_table_privilege('tappa_app','tenant_branding','DELETE')=false`; CHECK'ler hasmane değerlerle patlatılmış (küçük harf hex, 3 haneli hex, 262145 bayt, kısmi logo alanları); Down→Up→Down bayt-aynı; `GetTenantBrand` `logo` seçmiyor (test sorgu metnini okur) | WL-0 |
| WL-2 | `internal/brand/accent.go` | S | builder | tablo değerleri ±0,01, L sınırları 0,1790 ve 0,2368 dahil; palet `tailwind.config.js`'den pinli (renk değişirse kırmızı); `Suggest` deterministik, her çıktısı `Check`'ten geçer (özellik testi); kapsam ≥%90 | WL-0 |
| WL-3 | `internal/brand/logo.go` | M | builder | SVG/GIF/WebP/HTML/PNG-magic'li HTML red; 30000×30000 başlıklı dosya decode'dan ÖNCE red (bayt başına tahsis ölçülür); kesik red; IEND sonrası yük çıktıda yok; EXIF-GPS'li JPEG çıktısında `Exif` APP1 yok; CMYK JPEG, 16-bit, paletli, interlaced PNG normalize; ≤512 px, ≤256 KiB; `FuzzNormalize` panik yok, her başarılı çıktı yeniden decode olur ve sınırlarda; semafor -race altında ≤N; `go.mod` diff boş | WL-0 |
| WL-4 | Domain `internal/domain/tenant/brand.go` | M | builder | kaydet/sil UPDATE + audit aynı tx (zorla patlatılan audit UPDATE'i geri alır; iki yön); detail tam 6 sabit anahtar; domain accent'i yeniden `Check` eder → `ErrAccentIllegible`; `ActorID` zorunlu | WL-1,2,3 |
| WL-5 | Theme rotası + Tailwind token'ları (`brandtheme.go`, `tailwind.config.js`, `input.css`) | M | builder + tappa-brand | kurucu havuz almıyor; 200/404 matrisi (kanonik, küçük harf, geçersiz, red bandı); başlıklar birebir, gövde yalnız 3 özellik; marka yoksa tap butonu CDP computed `rgb(31,92,65)`/`rgb(255,253,244)`; `TestBrand_TenantAccentOnlyInBrandSlots` (mutasyon: `.stamp`'e `bg-brand` → kırmızı); `TestCompiledCSS_StampWordIsInk` yeşil; yorumdan ölü CSS kuralı doğmuyor | WL-2 |
| WL-6 | Logo rotaları + sayfa başına `img-src` | M | builder | başlıklar birebir; A oturumu B'nin sha'sını isteyince aldığı 404 bilinmeyen sha'nınkiyle bayt-aynı; oturumsuz tap logosu 404; "sayfa `img-src`'yi ancak `<img` içeriyorsa adlandırır" testi (panel + tap); ücretli istek sayısı ölçülüp bütçeler güncellendi (sıcak 1, soğuk 2) | WL-1,4 |
| WL-7 | Account → "Your brand" editörü (`brandactions.go`, account.templ/view, üç `ProtectWriting` rotası: logo, accent, sıfırla) | L | builder + tappa-brand | yükleme `MultipartReader` akış, `TMPDIR` salt-okunur/yokken bile başarılı (geçici dosya yok); yalnız tek `logo` parçası, bilinmeyen parça red; cross-origin POST resolver'dan önce red; manager POST 303 `not-permitted` + `brand_update_refused` + 0 UPDATE; okunaksız renk formu yeniden render + önerilen hex, yazma yok; önizleme gerçek tap bileşenlerini render eder; açık logo uyarısı (alfa ağırlıklı parlaklık paper'a <1,5:1 → uyarı, ret değil); `FactNoBulkImport` tripwire'ı "marka logosu handler'ı dışında multipart okuyucu yok" olarak yeniden türetildi (mutasyon: `employeeactions.go`'ya `FormFile` → kırmızı; SSS cümlesi değişmez); `<input type="color">` + hex alanı dokunma hedefi ≥44 px | WL-4,5,6 |
| WL-8 | Panel kabuğu (`panelChrome`, `PanelChrome`, `review.go` `chrome()`) | S | builder | logo + tenant adı + şerit (K4); `chrome()` tam +1 PK okuması, EXPLAIN ANALYZE seed'de <1 ms; marka okuma hatası sayfayı düşürmez; AdminChoose değişmez | WL-5,6, OP-10 |
| WL-9 | Tap, sonuç, tur ekranları (`base.templ` açık parametreli `BrandedPage…`, `tap.templ`, `result.templ`, `view.go`, `tap.go`/`checkin.go` `tapCSPFor`, `directory.go` `TapPage` markayı aynı tx'te okur, `result_test.go` beyaz listesi) | M | builder + tappa-brand | **§9 onayı (D-C) olmadan başlamaz**; `TapView` 3 ve `ResultView` 8 alan kalır (marka ayrı açık parametre; kartta onay alıntılı); logo `width`/`height` → CDP layout-shift 0; 390×844'te tap butonu üst kenarı ≤16 px kayar, hâlâ tek buton, "Tap" metni, ≥64 px; tek yeni metin `alt` = tenant adı; A çalışanı B plaketinde gövdede `/t/logo/` yok; marka yoksa HTML + CSP bayt-aynı (golden); okuma hatasında 200 + varsayılan | WL-5,6, D-C |
| WL-10 | Güvenlik denetimi (tüm WL diff'i) | M | tappa-security-auditor | ONAY — izolasyon (RLS + handler), fuzz/bomb, başlıklar, CSP diff, multipart tripwire, bütçeler, audit, log'da dosya adı/byte yok | WL-7,8,9 |
| WL-11 | E-posta entegrasyonu | S | builder | `"X\r\nBcc: y"` tenant adı ek başlık üretmez; RFC 2047; From alan adı hep Taptime | EM-7, WL-4 |
| WL-12 | Dokümanlar (tappa-brand skill tenant slotları + kontrast kuralı, handoff §4, roadmap, state) | S | orkestratör | `make check` + `make audit` exit 0 | WL-10 |

Bilinçli güncellenecek mevcut testler: `TestResultScreen_SaysExactlyThisAndNothingElse` (`alt`
metni), `FactNoBulkImport`, `TestBrand_*`, panel CSP ↔ script karşılığı testi.

## 6. Kararlar

**⏳ Şimdi kullanıcıya sorulan** (depo politikası / geri alınması zor mimari / §9 "önce sor" /
ürün önceliği):
| # | Soru | Öneri |
|---|---|---|
| D-A | Depo PUBLIC — ne yapılsın? Scrub push'u? | private yap + scrub'ı push et |
| D-B | Süper admin modeli | (c) hibrit: ayrı kimlik + TOTP + `ops.taptime.mt` + `op_*` definer'lar (B kararının yerine geçer) |
| D-C | Tap ekranı markası (§9) | logo + accent tap butonunda; sonuç ekranında yalnız logo |
| D-D | Akış sırası | Faz 0 → A1 → B → C → A2 (SES dış adımları paralel) |

**⏳ Sırası gelince sorulacak:** OP-K5 askıdaki ayın faturalanması · EM-K9 Mailpit (yeni dev aracı,
docker-compose, Go bağımlılığı değil) · OP-K12 T27/T37 (plan geçmişi, OP-17 öncesi) · OP-K2/K4
DNS + IP kısıtı ayrıntısı (D-B onaylanırsa).

**✅ Önerisiyle uygulanır** (otonomi kuralı; itiraz edilirse değişir):
- **Operatör:** K3 TOTP zorunlu (kurtarma CLI ile, kurtarma kodu yok) · K4 IP kısıtı opsiyonel ·
  K6 değiştiren eylemler tenant audit'inde de görünür, okumalar yalnız operatör log'unda · K7
  impersonation YOK · K8 encode Faz 4'te operatöre · K9 süreç ayrımı şimdilik yok · K10 operatör
  parolası ≥14 (tenant 8 kalır) · K11 geçişten sonra KF hesabı sıradan müşteri.
- **E-posta:** K1 `net/smtp` · K2 akış başına config · K3 adresi olmayan çalışan için owner-only
  "linki göster" yedeği · K4 yalnız EN · K5 sabit Taptime gönderen · K6 `mail.taptime.mt` · K7
  eu-central-1 · K8 bounce/complaint pilot sonrası · K10 SES TLS Require · K11 signup doğrulaması
  ayrı / pilot sonrası · K12 "parolanız değişti" bildirimi evet · K13 panel yedeği kalır.
- **White-label:** K1 tek accent + WCAG kapısı · K3 co-brand · K4 panelde 4 px şerit · K5 PNG/JPEG
  · K6 aktivasyon Taptime + işveren adı · K7 Postgres bytea · K8 e-postada faz 1 yalnız ad.

## 7. Kapsam dışı / riskler (özet)
- **Kapsam dışı:** tenant'a özel alan adı/subdomain (istenmedi; SUN URL'i çipte encode, `cookies.go`
  subdomain uyarısı) · tenant yazı tipi · karanlık tema · markalı fiziksel plaket · favicon/PWA ·
  impersonation · çoklu operatör rolleri · WebAuthn · kurtarma kodları · GDPR silme aracı (ayrı
  ADR) · rapor e-postası · Q28 uyarı teslimi · SMS · BIMI/MTA-STS · ayrı Deployment.
- **Riskler:** tek operatör tüm platform gücünü taşır (kontrol yalnız audit; iki kişi kuralı kapsam
  dışı) · TOTP gerçek zamanlı relay oltalamasına açık (WebAuthn sonraki iş) · aynı süreçte RCE iki
  DSN'i de ele geçirir (K9) · elle tiplenmiş definer çağrıları SQL'den sapabilir (testler sevk
  edilen SQL'i koşturur) · canlı kümede rol/Secret kurulumu kullanıcı işi (T44/T45 emsali) · iOS
  uygulama-içi tarayıcı aktivasyonu (EM-8 turu şart) · SES production access gecikmesi/reddi ·
  Hetzner 25/465 engeli (587 kullanılır, ilk gönderimde doğrulanır) · bastırılmış adrese gönderim
  sessiz (SNS yokken) · beyaz logo açık zeminde kaybolur (uyarı var) · soğuk önbellekte ücretli
  istek +1 · `/static` bugün cache/nosniff başlığı göndermiyor (ayrı sertleştirme işi).

## 8. Çalışma disiplini (değişmedi, bir ekle)
Her kod görevi: dal → yapıcı (opus) → her turda YENİ üçüncü göz → §4'e değince AYRICA
tappa-security-auditor, UI'ya değince tappa-brand; denetçiler sıralı; onay gelmeden done/commit
yok; commit öncesi `gofmt -l` + `make gen` + `make check`'in fmt/gen/diff yarısı (T72 kırmızısı
bunu atlamak için gerekçe DEĞİL — 25. oturumda CI'yı kırdı); **YENİ: commit öncesi sır taraması —
hiçbir dosyaya sır DEĞERİ yazılmaz (F0-5, A-0 dersi)**; main'e birleştirme = deploy = kullanıcı
kararı.
