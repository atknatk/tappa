# ADR 0031 — Tek kimlik, çok işletme: panel yöneticisi için global kimlik + işletme başına üyelik — CLAUDE.md §4.5'in üçüncü bilinçli istisnası

- **Durum:** **Kabul edildi — 2026-10-21.** Kullanıcı önce *"Önerilerin hepsini kabul"* dedi (K-1 …
  K-14'ün hepsi (a)). Aynı gün iki bağımsız denetim (güvenlik + üçüncü göz, 33 bulgu —
  orkestratörün `m14-plan-review-1.md` dosyası) **düzeltme gerekli** dedi; kullanıcı ardından
  **K-13'ü (b)'ye çevirdi** (*"üçüncü açık istisna"* — gerekçe §13). Bu sürüm 33 bulgunun hepsini
  işler; bir düzeltmenin kabul edilmiş bir K-kararının anlamını değiştirdiği yer §13'te kararın
  yanında **açıkça** yazılıdır. **Uygulama başlamadı** — kullanıcı *"başla"* diyene kadar kod,
  migration, CLAUDE.md değişikliği yok; CLAUDE.md metnini M14-01 bu ADR'nin §6.1'inden birebir
  uygular. Plan: [docs/plan/m14-tek-kimlik.md](../plan/m14-tek-kimlik.md).
- **Tarih:** 2026-10-21
- **Karar veren:** kullanıcı (ürün sahibi) — 2026-10-21: *"Hesapları birleştir (tek kimlik)"*: bir
  kişi = tek giriş kimliği, birden çok işletmeye (tenant) üye olabilir; ör. Kebab Factory ile Kebab
  Manufacturing arasında (aynı holding, ADR 0027) **çıkış yapıp yeniden girmeden** geçebilir.
  Kararlar: §13.
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) md.2/3/6/7 ·
  [ADR 0010](0010-deaktivasyon-tek-yonlu.md) ·
  [ADR 0013](0013-kayit-sihirbazi-ve-ilk-gelen-sirasi.md) ·
  [ADR 0014](0014-parola-digestinin-tabani-islenebilirlik.md) ·
  [ADR 0015](0015-sifirlama-tokeni-tek-gecislik-yetkidir.md) ·
  [ADR 0016](0016-tenant-kapsamsiz-operator-icerigi.md) ·
  [ADR 0020](0020-platform-operatoru-ayri-kimlik.md) ·
  [ADR 0021](0021-op-fonksiyonlari-tenant-otesi-erisim.md) ·
  [ADR 0022](0022-islemsel-eposta.md) · [ADR 0025](0025-tenant-askiya-alma.md) ·
  [ADR 0027](0027-holding-ve-capraz-tenant-dokunusu.md) ·
  [ADR 0028](0028-dis-api-anahtar-akis-webhook.md) · CLAUDE.md §4.5, §6, §7, §10 · M7-07
  ([m7-portal.md](../plan/m7-portal.md)) · EM-11 / EM-12 ([m10-platform.md](../plan/m10-platform.md))
- **Etkilenecek (uygulanınca):** yeni goose migration'ları (bugün son `00063`) ·
  `scripts/db-init/01-roles.sql` + canlı küme runbook'u (yeni definer rolü) · R5 muafiyet satırları
  (migration içinde) · `internal/db` katalog testleri (`privcatalog_test.go`, `operatorschema_test.go`,
  `m11schema_test.go`) · `internal/adminauth` · `internal/handler/adminlogin.go`, `logincontext.go`,
  `adminreset*.go`, `accountpassword.go`, `adminratelimit.go` ve panelin bütün POST formları ·
  `internal/domain/signup` · `web/templates` (kabuk, seçici, formlar, e-posta şablonları) ·
  `deploy/k8s/05-config.yaml` · CLAUDE.md §4.5 ve §6 · ADR 0002 / 0013 / 0015 / 0025 notları.

Atıflar (`yol:satır`) `main` `0e7c158`'e göredir.

---

## 1. Problem

Bugün bir panel yöneticisinin kimliği **bir işletmeye gömülüdür**. Aynı kişinin iki işletmesi varsa
iki ayrı `admin_users` satırı, iki parola, iki sıfırlama linki, iki oturumu vardır; işletme
değiştirmek için çıkış → giriş → "Which business?" seçicisi gerekir. Seçicinin metni bunu söyler:
*"you can sign out and switch at any time"* (`web/templates/pages/admin.templ:311`). M12 panolarındaki
kabuk-içi değiştirici (S1, S7) bu yüzden D1 ile kaldırıldı
(`docs/design/panel-redesign/screens/account-auth.md:29`, `:35`; `docs/backlog.md:294`).

Kullanıcı kararı bunu tersine çevirir: kimlik kişiye aittir, işletme bir **üyeliktir**.

## 2. Bugünkü model — kanıtla

| Olgu | Kanıt |
|---|---|
| `admin_users` tenant'a bağlı; e-posta **tenant başına** tekil, global değil | `db/migrations/00006_create_admin_users.sql:47-97` |
| `admin_users(id, tenant_id)`'ye **beş** bileşik FK: `admin_sessions`, `password_resets`, `billing_periods.closed_by`, `tenant_branding.updated_by`, `api_keys.created_by` | `00006:143-145`, `:194-196`; `00016:570-572`; `00028:126-128`; `00051:88-89` |
| FK'siz aktör sütunları: `audit_log.actor_id`, `transactions.entered_by`, `transaction_reviews.reviewer_id`, `policies.created_by`, `policy_versions.created_by` | `00005:115-118`, `:212-215`, `:264-267`; `00007:60`, `:122` |
| Giriş tek URL; e-posta **globalde** çözülür (`resolve_admin_by_email`, `tappa_resolver`), N aday döner; kullanıcı kararı 2026-08-02 "global çözümleme + tenant seçici" (KF ve KM aynı kişinin) | `00011:9-31`, `:315-350`; ADR 0002 `:80-87` |
| Çözümleyici incumbent-first (`ORDER BY a.created_at, a.tenant_id`), LIMIT yok | `00017:423-474` |
| Bir giriş en çok `MaxCandidates = 8` aday bcrypt'ler, her giriş 8'e **dolgulanır**; seçici `PickerCap = 7` | `internal/adminauth/manager.go:118`, `:156`, `:400-480` |
| Saldırı (00011 YÜKÜMLÜLÜK 5): saldırgan herkese açık kayıtla kendi tenant'ına **kurbanın e-postasını** yazar; seçici kurbanın işletmesini sunsaydı kurbanın tenant'ında `role=owner` oturum açılırdı. Kapanış: seçici yalnız parolası doğrulananlardan (`Verified`) | `manager.go:285-292`; `00011:131-160` |
| Seçici seti imzalı kısa ömürlü çerezde (`tappa_admin_choice`, 5 dk); POST'ta `selectVerified` | `internal/handler/logincontext.go:134`, `:205`, `:623` |
| Panel çerezi `tappa_admin_session`, `Path=/admin`, 12 saat ipucu; **sunucu tarafı son kullanma yok** | `internal/adminauth/cookie.go:27`, `:48`, `:57-75` |
| Her istek: `resolve_admin_session_by_token_hash` → `WithTenant(rs.TenantID)` içinde `TouchAdminSession` (yönetici `active`, iptal, askı) | `manager.go:851-905`; `db/queries/admins.sql:125-166`; `internal/httpx/adminidentity.go:128-174` |
| Tenant istemci girdisi değildir, oturumun **çıktısıdır** | `adminidentity.go:68-81`; `00011:24-31` |
| Panel yazmalarında **tenant'a bağlı bir senkronizör token yok** — `ProtectWriting` yalnız `sameOriginGate` taşır; bütün panel tek çerez, tek yol | `internal/handler/adminlogin.go:742-800`; `cookie.go:27`, `:48` |
| Oturum oran kovası **oturum id'sine** anahtarlı (`adminSessionLimit` 300/10 dk) | `adminlogin.go:694-711`; `adminratelimit.go:619-620` |
| Parola değişimi tek satırı, tek tenant'ta yazar; o tenant'taki öbür oturumları iptal eder | `internal/adminauth/changepassword.go:57-121`; `admins.sql:386` |
| `admin_users.email`'i yazan **hiçbir** sorgu yok; `UPDATE admin_users` yalnız `last_login_at` (`admins.sql:283`), parola (`admins.sql:386`), sıfırlama tüketimi (`passwordresets.sql:266`) | grep `db/queries` |
| Sıfırlama: bir adres → **satır başına bir link** (en çok 8), her link yalnız kendi satırını yazar (ADR 0015) | `internal/adminauth/resetrequest.go:50-68`, `:157-224`; `adminreset.go:601-603`; ADR 0015 `:32-45` |
| Giriş reddinin audit satırı **doğrulanan işletme sayısını** yazar (`VerifiedBusiness: len(verified)`) — bir tenant'ın sahibi kişinin kaç işletmesi olduğunu kendi günlüğünden okur | `adminlogin.go:1125`, `:1204` |
| `admin_users` satırı yaratan **tek** yol kayıt sihirbazı (`role='owner'`); yönetici daveti (M7-07 / EM-12) yapılmadı; yöneticiyi listeleme / devre dışı bırakma için panel yolu yok | `internal/domain/signup/signup.go:734-742`; `m7-portal.md:1509-1591`; `state.md:1653` |
| Kayıt sihirbazı VAT'ı global tekil yazar | `db/migrations/00001_*.sql:10-15` |
| Kabukta işletme adı **yalnız markalı** tenant'ta; `PanelChrome`'da tenant adı alanı yok | `admin.templ:453-504`, `:620-646`; `internal/handler/panelbrand.go:108-116` |
| Askı: yöneticiler girer ve okur, yazmalar 403; kapalı istisna listesi sekiz rota | ADR 0025 `:45-50`, `:374-387`, `:509-514`; `adminlogin.go:802-809` |
| Bayraklar `deploy/k8s/05-config.yaml`'dan gelir; dosya **her deploy'da** uygulanır, pod env'i `envFrom` ile **başlangıçta** okur | `.github/workflows/deploy.yml:808`; `deploy/k8s/20-app.yaml:282-284` |
| Migration Job rollout'tan **önce** koşar; eski pod yeni şemaya karşı hizmet vermeye devam eder (tek replika, `maxUnavailable: 0`) | `deploy.yml:887-896`, `:938`; `20-app.yaml:53-66` |
| Holding hiçbir panel kimliği vermez: MVP'de süper-admin / çapraz-tenant erişim yok; holding dışı tenant hiçbir yoldan erişemez | ADR 0002 md.6 `:286-288`; ADR 0027 `:881` |
| Operatör kimliği ayrı (`platform_admins`, ayrı host/çerez/TOTP); `admin_users.role`'e üçüncü değer girmez; hiçbir `op_*` yönetici yaratmaz/değiştirmez | ADR 0020 `:68-93`, `:144-146`, `:1094-1107`; `00029:159-169` |
| Definer sahiplerinin izinli kümesi `{tappa_resolver, tappa_opdefiner, tappa_holdingdefiner, tappa_feedwriter}` katalog testinde | `internal/db/operatorschema_test.go:513-517`, `:556-570`; `internal/db/m11schema_test.go:94` |

**Ölçülmeyen:** canlıda kaç e-posta birden çok satıra çözülüyor (`resetrequest.go:50-57`'nin
*"1,675"* sayısı geliştirme veritabanınındır). M14-02'nin işi; geçişin ön koşulu.

## 3. Hedef ve sınırlar

**Hedef.** (1) Bir kişi bir e-posta + bir parola ile girer. (2) Üye olduğu her işletme listelenir;
parola sormadan geçer. (3) Parola, sıfırlama, "her yerden çık" kişi başınadır. (4) Rol, durum ve
kayıtlar işletme başına kalır.

**Değişmeyenler.**
- Tenant verisi RLS altında; `tappa_app` NOBYPASSRLS, tablo sahibi değil; sorgularda açık `tenant_id`.
  Bir istek her an **tek** tenant bağlamında koşar.
- Tenant istemci girdisi olmaz. Geçişte hedef formdan gelir ama **yetki** oturumun sırrından ve
  hedefteki üyelik satırından türetilir; form *hangisini* seçer, *izin* vermez. Formlardaki yeni
  tenant bağı (§5.5) yalnız **reddetmek** için okunur, asla bağlam kurmak için.
- Operatör kimliği ayrı (ADR 0020); operatör kimlik gücü almaz.
- Holding üyeliği kimlik ya da üyelik ima etmez.
- Çalışan kimliği (`employees` / `sessions` / `tappa_session`) ve tap yolu etkilenmez.

## 4. Değerlendirilen seçenekler

- **A — Yeni `identities` + yeni `memberships`, `admin_users` emekli.** Beş bileşik FK ve beş FK'siz
  aktör sütunu taşınmak zorunda; `transactions` ve `audit_log` değişmez (§4.3, §4.6), geçmiş
  `entered_by` / `actor_id` yeniden yazılamaz. **Reddedildi.**
- **A′ — Tenant-kapsamsız `admin_identities`; `admin_users` üyelik olarak kalır (`identity_id`).**
  **Seçildi.** `admin_users.id` değişmez → bütün aktör referansları, FK'ler, RLS, rol kapıları aynen.
- **B — `admin_users` olduğu gibi + satırları bağlayan tablo.** İki parola, iki sıfırlama kalır; bağ
  tablosu da tenant-kapsamsızdır, §4.5 bedelini öder, kazancın yarısını verir. **Reddedildi.**
- **C — Şemasız: girişte doğrulanan seti oturum boyu taşı.** Yalnız aynı parolalı satırlar arasında
  geçiş; tek kimlik değil. **Reddedildi**; kullanıcı M14'ü ertelerse ara çözüm olarak açılabilir.
- **D — Tek satır, işletmeler bir dizi sütununda.** RLS'i kırar. **Reddedildi.**

## 5. Tasarım (A′)

### 5.1 Şema

**Tenant-kapsamsız tablolar** (CLAUDE.md §6 kuralları: Up bölümünde
`-- redline: no-tenant-scope(<tablo>) — <gerekçe>` (`scripts/redline-check.sh:511-517`,
`:1111-1135`), `tappa_app`'tan tablo **ve** sıra üzerinde açık `REVOKE ALL`, `ENABLE` + `FORCE ROW
LEVEL SECURITY` birlikte (`scripts/pg-restore-verify.sh:147-157`), politika yok, `tenant_id` adlı
sütun yok, silme yok):

| Tablo | Sütunlar (özet) | Not |
|---|---|---|
| `admin_identities` | `id`, `email citext NOT NULL UNIQUE`, `password_hash` (00018'in bcrypt CHECK'i), `email_verified_at NULL`, `last_used_tenant NULL`, `created_at`, `last_login_at` | **`status` sütunu yok** — M14'te kimliği kilitleyen bir yazar yoktur (bulgu 24); kilit gerekirse kendi ADR'siyle gelir |
| `admin_identity_resets` | `identity_id`, `token_hash` (00019 biçim CHECK'i), `created_at`, `expires_at` (00019 TTL tavanı), `used_at`, `cancelled_at` | kimlik başına sıfırlama |
| `admin_identity_verifications` | `identity_id`, `token_hash`, `expires_at`, `used_at` | var olan bir kimliğin e-postasını doğrulama (M14-18); **e-posta değiştirme M14'ün dışında** (K-15) |
| `admin_signup_claims` | `token_hash`, `email`, taslak (işletme adı, VAT, lokasyonlar, ad, parola digest'i), `expires_at`, `used_at` | doğrulama tıklamasından önce **hiçbir şey doğmaz** (K-9, bulgu 11); süre dolunca süpürülür |
| `admin_identity_events` | `identity_id NULL`, `event` (kapalı küme), `created_at`, `detail` (kapalı anahtarlar, e-posta yok) | kimlik düzeyi olaylar: kimliğe karşı başarısız giriş, sıfırlama talebi, doğrulama, birleştirme reddi. Append-only. Okuyan uygulama yolu yok (bulgu 17) |

**Tenant-kapsamlı tablo** (RLS beşlisi + GRANT, CLAUDE.md §6):

| Tablo | Not |
|---|---|
| `admin_invites` | `tenant_id`, `email`, `role`, `token_hash`, `expires_at`, `used_at`, `cancelled_at`, `invited_by` (bileşik FK → `admin_users(id, tenant_id)`). Owner kendi işletmesinin davetlerini RLS içinde listeler ve iptal eder (M7-07 kriteri). Kabul token-hash'e anahtarlı definer ile (§5.2) |

**`admin_users`** (sıcak tablo — her istekte `TouchAdminSession`):
- `identity_id uuid NULL REFERENCES admin_identities(id) ON DELETE RESTRICT` — **kendi
  `lock_timeout`'u olan** ayrı bir ifadeyle; `UNIQUE (tenant_id, identity_id)` indeksi **ayrı bir
  migration dosyasında** (`CONCURRENTLY`, goose no-transaction) — bulgu 19.
- `linked_via text NULL CHECK (linked_via IN ('backfill','reconcile','password','account','invite','signup'))`
  — bağın nasıl kurulduğu; yalnız definer yazar (bulgu 15). Backfill Down'ı yalnız
  `linked_via='backfill'` satırlarını çözer (bulgu 22).
- 🔴 **`tappa_app` `identity_id` ve `linked_via`'ya INSERT de UPDATE de edemez** (00017'nin sütun
  düzeyi GRANT kalıbı). Sebep `manager.go:285-292`'deki saldırının ikizidir: RLS bir tenant'ın kendi
  satırına istediği `identity_id`'yi yazmasına izin verirdi → saldırgan kurbanın kimliğini kendi
  işletmesine "üye" yapar, kurbanın değiştiricisine kendi işletmesini düşürürdü.
- `tappa_app`'ın `UPDATE (email)` yetkisi **expand'de** kapanır (hiçbir sorgu yazmıyor — §2) →
  K-14'ün aynası ilk günden yapısal (bulgu 21).
- `tappa_app` `SELECT (identity_id)`'yi **korur**: `TouchAdminSession` geçiş oran kovası için onu
  döndürür (§5.4). Bedel: tenant-ötesi bir korelasyon anahtarı uygulama belleğine girer. Sınır: bir
  sorgu-metni taraması `identity_id`'yi `TouchAdminSession` dışındaki **hiçbir** sqlc sorgusunun
  seçmediğini, bir görünüm-tipi taraması onun hiçbir şablon/CSV/API tipine girmediğini sabitler
  (bulgu 21'in "düşün" maddesi bu yönde karara bağlandı).
- **Bağlı satırı koruyan tetikleyici** (bulgu 6a): `BEFORE UPDATE ON admin_users`, `OLD.identity_id
  IS NOT NULL` ve (`password_hash` ya da `email` değişiyor) ve `current_user <>
  'tappa_identitydefiner'` → `RAISE` (fail-closed). Dual-write **tek yönlüdür: kimlik → satırlar**.
  Satır → kimlik eşitleyen bir tetikleyici **yazılmaz**: tenant A'nın bir yazması tenant B'nin
  satırına taşınırdı. **Adlandırılmış bedel:** expand rollout penceresinde eski pod'un bağlı bir
  satıra parola değişimi / sıfırlama tüketimi 500 alır (birkaç dakika; kabul).
- `password_hash`: bağlı satırda kimliğin digest'inin kopyası (dual-write, contract'a dek NOT NULL);
  davet ve kayıtla doğan bağlı satırlar da kopyayı taşır — **test edilen değişmez:** bağlı her satırda
  `password_hash = kimliğin digest'i` (bulgu 6).
- `full_name`, `role`, `status` üyelikte kalır (K-8, K-14).

**`admin_sessions`:** tek ekleme `authenticated_at timestamptz NULL` — oturumun parola kanıtının
anı. Girişte `now()`, geçişte **eski oturumdan kopyalanır** (bulgu 14). Oturum hâlâ tek üyeliğe,
tek tenant'a damgalıdır.

### 5.2 Roller ve fonksiyonlar — kapalı liste

**`tappa_identitydefiner`**: NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesi yok, varsayılan yetkisi yok,
yalnız sütun düzeyi GRANT; `01-roles.sql`'de yaratılır (migration'da değil — R5b, ADR 0021
`:759-763`) ve canlı kümeye runbook'la, **migration'dan önce** uygulanır (`tappa_holdingdefiner`
emsali, `01-roles.sql:133-154`). Katalog testlerindeki rol listeleri (`operatorschema_test.go:513-517`,
`:556-570`; `m11schema_test.go:94`) ve `privcatalog_test.go`'nun `privCatalogAllowed` izin listesi
(CLAUDE.md §6, T137) aynı commit'te genişler (bulgu 19).

**ADR 0002 md.7 çözümleyicileri** (salt okuma, sahibi `tappa_resolver`, ≤ 1 satır, UNIQUE'ten):

| Fonksiyon | Anahtar | Dönüş |
|---|---|---|
| `resolve_identity_by_email(p_email citext)` | e-posta | `(id, password_hash, email_verified_at)` — giriş. Global UNIQUE sayesinde md.7 (ii) "≤ 1" giriş için geri gelir (bugün e-posta istisnadır, ADR 0002 `:316-322`) |
| `resolve_identity_reset_by_token_hash(p_hash)` | sıfırlama token hash'i | `(id, identity_id, expires_at, used_at, cancelled_at)` |
| `resolve_admin_invite_by_token_hash(p_hash)` | davet token hash'i | `(id, tenant_id, role, expires_at, used_at, cancelled_at)` |
| `resolve_signup_claim_by_token_hash(p_hash)` | kayıt token hash'i | `(id, expires_at, used_at)` |
| `resolve_identity_verification_by_token_hash(p_hash)` | doğrulama token hash'i | `(id, identity_id, expires_at, used_at)` |

**`identity_*` fonksiyonları** (sahibi `tappa_identitydefiner`; yazarlar). **Kural: kimliği yazan her
fonksiyon kimliği yalnız bir sırdan bulur** — canlı oturum (`admin_sessions.token_hash` →
`admin_users.identity_id`, **NOT NULL** olmalı) ya da sıfırlama / davet / kayıt / doğrulama
token'ı. **E-postadan kimliğe giden tek yol `resolve_identity_by_email`'dir ve hiçbir şey yazmaz**
(bulgu 2).

| Fonksiyon | Anahtar | Ne yapar / ne zaman reddeder |
|---|---|---|
| `identity_open_session(p_identity_id, p_tenant, p_token_hash)` | ⚠️ **sır değil — adlandırılmış istisna 1** | Parolası uygulamada doğrulanmış kimlik için istenen / son kullanılan / en eski aktif üyelikte oturum açar, `authenticated_at = now()`, o tenant'a giriş audit'i. Tek çağrı yolu Go'daki doğrulanmış-kimlik tipidir (00011 YÜKÜMLÜLÜK 5'in karşılığı; bugünkü `Issue(Verified)` ile aynı güven) |
| `identity_create_for_signup(p_tenant_id, p_admin_user_id)` | ⚠️ **sır değil — adlandırılmış istisna 2, yalnız expand → M14-17 arası** | Kayıt transaction'ı içinde: e-posta **hiçbir** kimlikte ve bu satır dışında **hiçbir** `admin_users` satırında yoksa kimlik yaratır + bağlar (`linked_via='signup'`); varsa **hiçbir şey** yaratmaz/bağlamaz, satır bağlanmamış doğar (500 yok — `signup.go:733-742`'de global UNIQUE çakışması oluşmaz). M14-17 onu `identity_claim_signup` ile değiştirir ve **DROP** eder |
| `identity_memberships(p_session_hash)` | canlı oturum | Kişinin aktif üyelikleri, ≤ 50, `(tenant_id, tenant_name, role, suspended, is_current)`; oturumun satırı bağlı değilse yalnız kendisi |
| `identity_switch_session(p_session_hash, p_target_tenant, p_new_hash)` | canlı oturum | §5.4 |
| `identity_revoke_sessions(p_session_hash, p_keep_current)` | canlı oturum | Kişinin **bütün** işletmelerdeki oturumlarını iptal; her tenant'a kendi çıkış satırı |
| `identity_password_for_session(p_session_hash)` | canlı oturum | Hesap sayfasında mevcut parolayı doğrulamak için digest; satır bağlı değilse 0 satır (uygulama bugünkü tek-satır yolunu kullanır) |
| `identity_set_password(p_session_hash, p_new_hash)` | canlı oturum | Kimliğe **ve** bağlı bütün satırlara yazar, öbür oturumları bütün işletmelerde iptal eder; satır bağlı değilse **0 satır** ve uygulama parolayı **yalnız o satıra** yazar (bugünkü gibi) |
| `identity_consume_row_reset(p_reset_hash, p_new_hash)` | **eski** `password_resets` token'ı | M14-08'den itibaren: link bağlı bir satırınsa kimliği + bağlı satırları yazar, bütün oturumları iptal eder (ADR 0015'in anlam değişikliği **burada başlar**, bulgu 6); bağlı değilse 0 satır (eski yol) |
| `identity_consume_reset(p_reset_hash, p_new_hash)` | kimlik sıfırlama token'ı | M14-14: kimlik + bağlı satırlar, `email_verified_at`, bütün oturumlar iptal, oturum vermez; bağlanmamış eski satırları **bağlamaz** |
| `identity_link_legacy(p_identity_session_hash, p_legacy_session_hash)` | **iki** canlı oturum | Biri bağlı (kimlikli), öbürü bağlanmamış satırın oturumu; ikisi de canlı → iki kimlik bilgisinin kanıtı **veritabanında yapısal** (bulgu 9). Savunma katmanı: iki satırın e-postası eşit (`OPERATOR(public.=)`); eşit değilse 0. Bağlanan satırın digest'i kimliğinkine eşitlenir, eski oturumu iptal edilir, o tenant'a "linked" (karşı tenant'ı anmadan) |
| `identity_unlink_backfilled(p_session_hash, p_tenant)` | canlı oturum | Yalnız `linked_via IN ('backfill','reconcile','signup')` ve kimlik **ilk kez** doğrulandıktan sonraki onay ekranından (bulgu 15); satırı bağlanmamışa çevirir, o tenant'taki oturumları iptal eder |
| `identity_verify_email(p_verification_hash)` | doğrulama token'ı | `email_verified_at`; ilk doğrulamaysa onay listesini açar |
| `identity_accept_invite(p_invite_hash, p_session_hash)` | davet token'ı + canlı oturum | Var olan kimlik: kimlik **doğrulanmış** ve `identity.email = invite.email` (`OPERATOR(public.=)`) değilse 0 satır (iletilmiş link, bulgu 12); üyelik doğar (`linked_via='invite'`, digest kopyası); **yeni oturum vermez**, kişi değiştiriciyle geçer |
| `identity_accept_invite_new(p_invite_hash, p_new_hash)` | davet token'ı | Adreste kimlik yoksa: yeni kimlik (`email_verified_at` = tıklama) + üyelik; **oturum vermez** (ADR 0015 §1 deseni), kişi giriş sayfasına gider. Adreste kimlik varsa 0 satır |
| `identity_open_signup_claim(...)` / `identity_claim_signup(p_claim_hash, p_session_hash NULL)` | kayıt token'ı (+ var olan kimlikse canlı oturum) | M14-17 (§5.10) |

Her fonksiyon: `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, şema-nitelikli gövde,
dinamik SQL yok, sabit dönüş sütunları, `REVOKE ALL … FROM PUBLIC`, `EXECUTE` **yalnız**
`tappa_app`; `tappa_operator` ve `tappa_opdefiner` hiçbir `identity_*` / `resolve_identity_*`
fonksiyonunu çalıştıramaz ve hiçbir kimlik tablosunda yetki taşımaz (katalog testi
`has_function_privilege` / `has_any_column_privilege`). Tenant'a yazan her fonksiyon **o tenant'ın**
`audit_log`'una yazar ve satır karşı tenant'ı anmaz. **İmza tablosu** (`pg_get_function_identity_arguments`
+ `pg_get_function_result` metni) ve `tappa_identitydefiner`'ın **fiil × sütun matrisi** ilk
migration'da üretilir ve ADR 0027 §2/§3'teki gibi katalog testiyle birebir pinlenir: listeye fonksiyon
eklemek, dönüş sütununu ya da matrisi genişletmek **kullanıcı onayı + bu ADR'nin değişikliği** ister.

### 5.3 İstek başına yol

Çerez → `resolve_admin_session_by_token_hash` → `WithTenant(rs.TenantID)` → `TouchAdminSession`.
`admin_identities`'e istek başına okuma **yok**. `TouchAdminSession`'ın `RETURNING`'ine iki sütun
eklenir — `tenants.name` (kabukta işletme adı, ek okuma yok) ve `admin_users.identity_id` (geçiş oran
kovası). Askı kapısı, `ProtectWriting`, `RequireAdmin`, ~50 `AdminOf` çağrı yeri ve rol kapıları
dokunulmadan çalışır; var olan oturumlar deploy'u atlatır.

### 5.4 Geçiş

`POST /admin/switch` (form: `tenant_id`, `csrf`). Zincir `protectWritingThroughSuspension`
(`adminlogin.go:802-809`: flood → sameOrigin → requireAdmin → session) + **senkronizör token**:
`HMAC(oturum anahtarından türetilmiş anahtar, "tappa/admin-switch/v1|" + session_id)`, sayfa
işlenirken forma yazılır, POST'ta sabit-zamanlı karşılaştırılır; oturum değişince token da değişir.
Tek definer çağrısı, tek transaction:

1. Mevcut oturum canlı, satırı `active` ve **bağlı**.
2. Hedefte aynı `identity_id`'li `active` üyelik; yoksa 0 satır → tek tip "bu işletmeye
   geçilemiyor" (var olmayan / üye olunmayan / devre dışı ayırt edilmez).
3. Hedefte yeni oturum (yeni token'ın hash'i; `authenticated_at` eskiden kopyalanır).
4. Eski oturum `revoked_at` (00011'in monotonik tetikleyicisi).
5. `last_used_tenant`.
6. **İki audit satırı, birbirini anmadan, sıradan satırlarla bayt-aynı** (bulgu 17): A'ya sıradan bir
   **çıkış** satırı, B'ye sıradan bir **giriş** satırı; `via=switch` **yazılmaz** (B'nin sahibi kişinin
   başka bir işletmesi olduğunu öğrenmesin). Metin taraması testi iki satırda karşı tenant id'sini,
   adını, üyelik sayısını ve `identity_id`'yi yasaklar.

**Eşzamanlı geçiş** (bulgu 29): aynı oturumla N paralel istek → tam olarak bir kazanan; kaybedenlerin
yanıtı **`Set-Cookie` yazmaz ve çerezi silmez** (kazananın çerezi tarayıcıda kalır).

**Oran sınırı** (bulgu 8): kova `identity_id`'ye (+ kaynak adres) anahtarlı — oturum id'sine değil,
çünkü her geçiş yeni oturumdur ve oturum kovası sıfırlanırdı. Aşılınca 429, audit yazılmaz.

**Askı:** hedef askıdaysa geçiş serbest (ADR 0025 `:509-514` — giriş ve okuma açık); panel o
işletmede salt okunur. ADR 0025'in sekiz rotalık istisna listesi (`:374-387`) **birden fazla** rota
büyür (bulgu 16): `/admin/switch`, "Sign out everywhere", "Link a business", e-posta doğrulama
talebi — hepsi `protectWritingThroughSuspension` grubunda (kendi parolası rotasının yanında).
M14-01'in ADR 0025 notu bu listeyi yazar.

### 5.5 Eski sekmeden yanlış işletmeye yazma — tenant'a bağlı formlar (bulgu 3)

Bütün panel tek çerez, tek yol (`Path=/admin`); A'da açık kalmış bir sekmenin formu, B'ye geçildikten
sonra gönderilirse **B'ye yazardı** — bugün panel yazmalarında tenant'a bağlı bir senkronizör yok
(`adminlogin.go:742-800`). Kapanış (M14-10a, M14-10'dan **önce**): her panel POST formu sayfa
işlendiği andaki tenant'ın bağını gizli alan olarak taşır
(`HMAC(türetilmiş anahtar, "tappa/admin-form-tenant/v1|" + tenant_id)`); `ProtectWriting` ve
`protectWritingThroughSuspension` herhangi bir yazmadan önce alanı oturumun tenant'ıyla karşılaştırır;
uyuşmazsa 409 ve *"This page belongs to another business — reload it."* Alan **yalnız reddetmek**
için okunur; bağlam asla ondan kurulmaz (§3). Paneldeki htmx yalnız GET taşır (ör.
`web/templates/pages/transactions.templ:601`), yazma yolu formlardır.

### 5.6 Giriş

Bayrak açıkken: e-posta → `resolve_identity_by_email` (≤ 1) → kimlik digest'i karşılaştırılır →
`identity_open_session` → `/admin`; iniş `last_used_tenant`, değilse en eski aktif üyelik (K-4).

**Geçiş dönemi eski kolu:** aynı e-postalı **bağlanmamış** satırlar bugünkü gibi (en çok 8,
incumbent-first) karşılaştırılır. **Zamanlama dört durumda da düz** (kimlik var/yok × bağlanmamış satır
var/yok, bilinmeyen e-posta dahil): her giriş contract'a dek **tam 1 + `MaxCandidates`**
karşılaştırma öder (bulgu 10); `adminLoginWorkLimit` (`adminratelimit.go:737`) 9 karşılaştırmaya göre
yeniden boyutlanır. Yazılan parola hem kimliği hem bir eski satırı doğrularsa: önce eski satırda bir
oturum, sonra kimlikte bir oturum açılır, `identity_link_legacy` iki oturumun hash'iyle çağrılır, eski
oturum iptal edilir (`linked_via='password'`). Yalnız eski satır(lar) doğrularsa kişi bugünkü gibi
kimliksiz girer — **birden çok eski satır doğrularsa seçici contract'a dek kalır** (K-4'ün "seçici yok"
ifadesi yalnız kimlik yoluna aittir; bulgu 10).

Başarısız kimlik girişleri `admin_identity_events`'e yazılır; tenant audit satırlarından doğrulanan
işletme **sayısı** kaldırılır (`adminlogin.go:1125`, `:1204` — bugünkü sızıntı). `adminAccountLimit`
(`adminratelimit.go:740-781`) kimlik yolunda `identity_id`'ye anahtarlanır.

### 5.7 Birleştirme

**Kural: iki satır yalnız kişi her ikisinin kimlik bilgisini kanıtlarsa birleşir** (K-2). "Aynı
e-posta" kanıt değildir (00011 saldırısı; kayıt herkese açık, doğrulama yok — ADR 0013 `:304-316`).
Kanıt = **iki canlı oturum** (`identity_link_legacy`):
1. **Girişte otomatik** (§5.6) — aynı parola.
2. **Hesap sayfasında** — kimlik oturumuyla eski parola ya da eski oturumla kimlik parolası
   (en çok 8 aday, dolgulu); doğrulanan her satır için kısa ömürlü bir oturum açılır, bağlanır, iptal
   edilir. Sonuç tek tip ("bağlanacak işletme yok" ile "parola yanlış" ayırt edilmez).
3. **Operatör birleştiremez** (K-12; ADR 0020 `:1094-1095`).

### 5.8 Backfill ve uzlaştırma

**Backfill** (M14-06): her dolu e-posta grubu için — tek satır → kimlik + bağ; çok satır → **en
eski aktif** satırdan (yoksa en eskiden) kimlik + yalnız o satır bağlanır (`linked_via='backfill'`),
öbürleri bağlanmamış kalır. E-postasız satıra kimlik yok. Migration başında rolün superuser ya da
BYPASSRLS olduğu doğrulanır (FORCE RLS altında 0 satırı sessizce görürdü); sonunda bağlı sayı > 0
doğrulanır (bulgu 22).

**Uzlaştırma** (M14-06b, idempotent, aynı incumbent-first kuralı): backfill'den sonra doğan satırlar
(eski pod penceresi, expand ile M14-17 arasındaki kayıtlar) için kimlik yaratır/bağlar
(`linked_via='reconcile'`). Expand'den **sonraki** deploy'da ve M14-19'dan **hemen önce** koşar
(bulgu 6b, 23). Binary'yi expand'in altına geri almak yalnız uzlaştırmayla birlikte.

**İlk doğrulamada onay** (bulgu 15): `email_verified_at IS NULL` bir kimlik ilk kez doğrulandığında
(sıfırlama, doğrulama linki, davet) `linked_via IN ('backfill','reconcile','signup')` üyelikleri
listelenir; kişi her birini onaylar ya da çözer. Bu, *"daha önce kaydolmuş saldırgan"* durumunu
(ADR 0013 risk 3) kapatır: e-postanın gerçek sahibi sıfırlamayla kimliği alır ve saldırganın
işletmesini kendi değiştiricisinden çıkarır.

### 5.9 Parola sıfırlama

M14-08'den itibaren **bağlı** bir satırın eski linki kimliği yazar (`identity_consume_row_reset`) —
ADR 0015'in *yetkinin nesnesi* değişikliği bayrağa değil **dual-write'a** bağlıdır. M14-14'ten
itibaren talep e-posta → kimlik → **tek** link; bağlanmamış satırlar contract'a dek satır başı link
alır. ADR 0022 §3/§4/§9/§10 ve EM-7C'nin "hesap başına tek canlı link" kuralı aynen.

### 5.10 Kayıt sihirbazı (K-9 a)

Doğrulama tıklamasından önce **kimlik, tenant ve üyelik doğmaz** (bulgu 11). `POST /signup` taslağı
`admin_signup_claims`'e yazar (parola digest'i dahil, TTL ile), adrese link gönderir ve her durumda
aynı *"Check your inbox"* ekranını gösterir. Tıklama:
- adreste kimlik yoksa → tenant + kimlik (`email_verified_at` dolu) + owner üyeliği, tek transaction;
  VAT o anda yazılır — iki bekleyen kayıt aynı VAT'ı taşıyorsa ikincisi bugünkü VAT çakışması
  mesajını alır; süresi dolan talep hiçbir şey ayırmaz (VAT'ı tutmaz);
- adreste kimlik varsa → kişi **kendi parolasıyla** girer (canlı oturum) ve yeni işletmeyi sahiplenir;
  **doğrulanmamış** kimlik sahiplenemez → önce sıfırlama (adrese sahiplik) gerekir.
Oranlar: `signupCreateLimit` (adres başına) + **alıcı başına** üst sınır (EM-7C'nin `(mailbox)` kovası
biçiminde) (bulgu 11, 32). `signInBlocked` kimlik yolunda emekli.

**Sıralama (bulgu 7):** bayrak açıkken ama M14-17'den önce bir kayıt, var olan e-posta için
"bağlanmamış satıra iniş" ile yeni e-posta için "kimliğe iniş" arasındaki farkla adresin varlığını
söylerdi — ADR 0013'ün fiyatlanmış kahininden (`:271-323`) **ucuz** bir kahin. Bu yüzden **M14-17,
M14-19'un (bayrağı açma) bağımlılığıdır**.

### 5.11 Davet

Owner-only (policy motoru; `m7-portal.md:1570-1573`'ün `deny sid=default` tuzağı), tenant-kapsamlı
`admin_invites` (owner listeler ve iptal eder), tek kullanımlık, süreli, hash'li, davetlinin adresine
gider, panelde gösterilmez. Kabul §5.2'deki iki fonksiyonla; rızasız üyelik doğmaz; doğrulanmamış
kimliğe davet önce sıfırlama ister; holding üyeliği davetin yerine geçmez.

### 5.12 Devre dışı bırakma, sahiplik, operatör

- **Üyelik başına** (K-6): owner o işletmedeki üyeliği `disabled` yapar → yalnız o tenant'taki
  oturumlar düşer. Kimliği kilitleyen bir yol M14'te **yoktur** (bulgu 24).
- **Son owner değişmezi** ve iki adımlı devir (M14-16).
- **Contract ön koşulu** (bulgu 13): *"tek aktif owner'ı bağlanmamış olan tenant sayısı = 0"*
  (runbook, yalnız sayı); 0 değilse contract durur.
- **Operatör değişmez** (K-12): kimlik tablolarına ve fonksiyonlarına yetki yok. ADR 0025'in ileride
  gelecek `op_disable_tenant_admins`'i (`:620-621`) yalnız üyelik `status`'una dokunabilir, kimliğe
  asla (bulgu 25).

### 5.13 API anahtarları, webhook'lar, aktörler

Değişmez: `api_keys.created_by` üyeliğin bileşik FK'si (`00051:88-89`); devre dışı bir üyeliğin
anahtarları bugünkü gibi çalışır (ADR 0028 ele almıyor — backlog notu). Webhook'ların aktörü yok
(`00048:186-199`). Aktör id'leri üyelik id'si kalır → bir tenant'ın günlüğü başka tenant'taki üyelik
id'sini görmez.

### 5.14 E-posta şablonları (bulgu 32)

Yeni ya da değişen şablonlar, hepsi ADR 0022 §4 (sabit ASCII konu, alıcı kuralı), §3 (sızıntı), §9
(oranlar) ve EM-7C sınırlarıyla: **yönetici daveti** · **kayıt doğrulaması** (yeni adres) · **var olan
adresle kayıt** ("bu adresle bir işletme kaydı başlatıldı — sizseniz giriş yapıp sahiplenin") ·
**e-posta doğrulaması** (var olan kimlik) · **kimlik başına sıfırlama** · **kişi başına "parolanız
değişti"** (EM-9; bütün işletmelerde bir kez).

### 5.15 Log ve izleme

CLAUDE.md §7: e-posta, token, digest log'a yazılmaz; log anahtarları ADR 0022 §10'un kapalı kümesi +
`identity_id`. **Contract hazırlık ölçüsü** (bulgu 33): e-postasız SigNoz sayaçları — kimlik yoluyla
giriş, eski kolla giriş, geçiş reddi, bağlama — log olay adlarından hesaplanır; eski kol sayacı sıfıra
yaklaştığında contract konuşulur.

## 6. §4.5 — üçüncü bilinçli istisna (K-13 = b)

| Katman | Durum |
|---|---|
| Tenant verisi (`admin_users`, `admin_sessions`, `admin_invites`, iş tabloları) | RLS + FORCE + açık `tenant_id`, değişmez |
| Kişi verisi (§5.1'in beş tablosu) | Tenant-kapsamsız; `tappa_app`, `tappa_operator`, `tappa_opdefiner` yetkisiz; yalnız `tappa_identitydefiner` / `tappa_resolver` sahipli kapalı liste |
| Tenant sınırını geçen hareketler | Giriş, geçiş, davet kabulü, kayıt sahiplenme, bağlama, sıfırlama — her biri bir **sırra** anahtarlı (iki adlandırılmış istisna hariç), sonucu tek-tenant bir oturum ya da tek-tenant satır yazmaları. Bir istek asla iki tenant'ta koşmaz |
| `tappa_app`'ın kimlik bağı yazması | Sütun düzeyinde kapalı + bağlı satır tetikleyicisi |
| Çapraz sızıntı | Audit satırları karşı tenant'ı, üyelik sayısını, `identity_id`'yi anmaz; liste yalnız kişinin kendi oturumuyla |

### 6.1 M14-01'in CLAUDE.md'ye uygulayacağı metin (birebir)

§4.5'te *"**Bilinçli istisnalar iki tanedir** — tenant sınırını aşmanın yalnız bu iki yolu var:"*
→ *"**Bilinçli istisnalar üç tanedir** — tenant sınırını aşmanın yalnız bu üç yolu var:"*;
*"Bu iki istisnanın dışında kalan dış akış yazıcısı"* → *"Bu üç istisnanın dışında kalan dış akış
yazıcısı"*; (2) maddesinin sonundaki parantezden sonra şu madde eklenir:

> **(3) Panel yöneticisinin kimliği** (ADR 0031) — bir kişi tek kimlikle birden çok işletmeye üye
> olur. Kişi verisi tenant kapsamsız beş tabloda durur (`admin_identities`, `admin_identity_resets`,
> `admin_identity_verifications`, `admin_signup_claims`, `admin_identity_events`); `tappa_app`'ın
> bunlarda hiçbir yetkisi yoktur ve `admin_users.identity_id` / `linked_via`'yı yazamaz. Erişim yalnız
> ADR 0031 §5.2'deki **kapalı** `identity_*` listesiyle (sahibi `tappa_identitydefiner`: NOLOGIN,
> BYPASSRLS, NOSUPERUSER, üyesiz) ve salt okuma `resolve_identity_*` / `resolve_admin_invite_*` /
> `resolve_signup_claim_*` çözümleyicileriyle (`tappa_resolver`, ADR 0002 md.7) olur. Kimliği yazan
> her fonksiyon kimliği bir **sırdan** bulur (canlı oturum, sıfırlama/davet/kayıt/doğrulama token'ı);
> adlandırılmış iki istisna `identity_open_session` (Go tipiyle bağlı, 00011 YÜKÜMLÜLÜK 5'in
> karşılığı) ve geçici `identity_create_for_signup`'tır (M14-17'de kalkar). Bir tenant'a yazan her
> fonksiyon o tenant'ın `audit_log`'una yazar ve karşı tenant'ı anmaz; bir istek her an tek tenant
> bağlamında koşar. `tappa_operator` ve `tappa_opdefiner` hiçbir `identity_*` fonksiyonunu çalıştıramaz
> ve kimlik tablolarında yetki taşımaz. **Listeye fonksiyon eklemek, dönüş sütunlarını ya da
> definer'ın sütun matrisini genişletmek kullanıcı onayı + ADR 0031 değişikliği ister** (testler: ADR
> 0031 §5.2'deki imza katalog testi ve sütun matrisi testi).

§6'nın *"**Tenant kapsamsız tablo** (yalnız platform operatörü — ADR 0020/0021 — ve holding
üyeliği: …)"* parantezi şöyle genişler: *"(yalnız platform operatörü — ADR 0020/0021 —, holding
üyeliği: `holdings`, `holding_members`, yazarı yalnız `op_*` — ADR 0027 — ve panel yöneticisinin
kimliği: `admin_identities`, `admin_identity_resets`, `admin_identity_verifications`,
`admin_signup_claims`, `admin_identity_events`, yazarı yalnız `identity_*` — ADR 0031)"*.

§3 dizin haritasına `internal/adminauth` için bir satır eklenir: *"panel yöneticisinin kimliği ve
oturumu (ADR 0031); kimlik tablolarına yalnız `identity_*` / `resolve_identity_*` ile gider"*.

## 7. `manager.go:285-292` saldırısı ne olur

| Aşama | Durum |
|---|---|
| Bugün | `Verified` + imzalı set + `selectVerified` ile kapalı; yan bedeller: DoS (`MaxCandidates`), sayım ve zamanlama kanalları, kalabalıklaştırma |
| Expand, bayrak kapalı | Davranış aynı. Kayıt var olan e-postada kimlik yaratmaz (`identity_create_for_signup` reddi); `identity_id` uygulamaya yazılamaz |
| Expand + bayrak açık (yalnız M14-17'den **sonra**) | E-posta tek kimliğe çözülür; saldırganın ektiği satır bağlanmamış eski satırdır, eski kol bugünkü korumalarıyla sürer; kayıt doğrulamasız kimlik doğurmaz → bulgu 7'nin ucuz kahini oluşmaz |
| İlk doğrulama | Backfill / uzlaştırma / kayıtla bağlanmış üyelikler kişiye onaylatılır (§5.8) |
| Contract | Eski kol kalkar; `admin_users.email` ve `password_hash` doğrulamada okunmaz → sınıf **yapısal olarak** kapanır; `MaxCandidates`, dolgu, `PickerCap`, `signInBlocked`, ADR 0013'ün ön-kayıt sınırı emekli |

`manager.go:285-292` yorumu contract'ta silinmez, tarihsel olarak işaretlenir ve bu ADR'ye bağlanır.

## 8. Geçiş — expand → bayrak → contract (iki deploy)

**Bayrak** `TAPPA_ADMIN_IDENTITY` (`off` | `on`): `deploy/k8s/05-config.yaml`'da `off` olarak doğar,
`internal/config` onu doğrular, paketleme testleri (ör. `TestPackaging_NoConfigMapKeyIsAlsoAnEnvEntry`)
güncellenir. Dosya her deploy'da uygulanır (`deploy.yml:808`) ve pod env'i başlangıçta okur
(`20-app.yaml:282-284`): **bayrağı çevirmek = `05-config.yaml` commit'i + `main` deploy'u = kullanıcı
kararı.** `kubectl edit` ile yapılan bir geri dönüş bir sonraki deploy'da sessizce geri çevrilir;
runbook bunu yasaklar (bulgu 5).

1. **Rol** canlı kümeye (runbook, `tappa_owner`) — migration'dan önce.
2. **Expand deploy**: eklemeli migration'lar (tablolar, sütunlar, tetikleyici, fonksiyonlar,
   backfill) + yeni binary, bayrak `off`, dual-write açık. **Eski binary yeni şemada** çalışır (kayıt
   INSERT'i `identity_id NULL` ile; bağlı satır parola yazması tetikleyiciyle 500 — adlandırılmış bedel).
3. **Uzlaştırma deploy'u** (M14-06b) — pencerede doğan satırlar.
4. **P5'in M14-17'si** canlıda (bayrak hâlâ `off`).
5. **M14-19 bayrak `on`**: ön kontroller — kimliği olmayan dolu e-posta = 0; `count(identity_id IS NOT
   NULL AND password_hash <> kimliğin digest'i) = 0`; uzlaştırma yeniden koşturuldu. Geri dönüş =
   `off` commit'i + deploy.
6. **Soak** — K-3'ün 60 günü **M14-14 ve M14-18 canlıya çıktığı gün** başlar (bulgu 20).
7. **Contract A** (M14-20a): binary eski kolu çağırmayı bırakır, bayrak kaldırılır; **şema
   değişmez** → bu deploy'un rollout penceresindeki eski pod hâlâ çalışır.
8. **Contract B** (M14-20b, **sonraki** deploy): `DROP FUNCTION resolve_admin_by_email`; `tappa_app`'tan
   `INSERT (email, password_hash)` ve `UPDATE (password_hash)` `REVOKE`; bayat digest kopyaları `NULL`;
   `password_hash` NULL'lanabilir; `CHECK (identity_id IS NOT NULL) NOT VALID` (yeni satırlar için).
   Rollout penceresindeki eski pod (contract A binary'si) bunların hiçbirini çağırmaz (bulgu 4).

**Down'lar:** expand Down'ı yalnız eklediğini kaldırır (backfill Down'ı `linked_via='backfill'`).
Contract B'nin Down'ı kimlik digest'ini satırlara geri kopyalayabilir; geri alınamayan şey **silinen
koddur**, ve contract'tan sonra davetle doğmuş `password_hash NULL` satırlar `NOT NULL` geri gelmeden
önce doldurulmalıdır (bulgu 28).

## 9. Testler (özü — adlar yapıcının)

- **RLS (filtresiz sonda):** A'nın bağlantısı aynı kimliğin B'deki `admin_users` / `admin_invites`
  satırını görmez; pozitif kontrol.
- **Yetki:** `tappa_app`, `tappa_operator`, `tappa_opdefiner` için beş kimlik tablosunda
  `has_any_column_privilege = false`; `identity_*` için `has_function_privilege('tappa_operator', …) =
  false`; `tappa_app` `identity_id` / `linked_via` / `email` yazamaz.
- **Katalog:** imza tablosu, fiil × sütun matrisi, sahip kümesi, `search_path`, PUBLIC EXECUTE yok,
  rol üyesi 0, `rolsuper=f`.
- **Geçiş:** üyeliği olmayan / devre dışı / iptal edilmiş / başka kimliğin oturumu / bağlanmamış
  satırın oturumu → 0; başarıda eski oturum iptal, yeni token, `authenticated_at` kopyası, iki audit
  satırı sıradan satırlarla bayt-aynı; A'da işlenmiş form B'ye geçtikten sonra gönderilince B'de 0 satır.
- **Dual-write:** bağlı satıra doğrudan parola/e-posta yazması reddedilir; bağlı her satırda digest
  eşitliği değişmezi; mevcut e-postayla kayıt kimlik digest'ini bayt-aynı ve `identity_id`'yi NULL
  bırakır.
- **Mutasyon kanıtları:** geçişte kimlik eşitliği · `status='active'` · eski oturum iptali · sütun
  GRANT'ı · tetikleyici · e-postayla çözülen dual-write · bağlamada ikinci oturum şartı · audit'e hedef
  tenant · oran kovası oturum id'sine dönünce → her biri kırmızı.
- **Eşzamanlılık:** N paralel geçiş → bir kazanan, kaybedenler `Set-Cookie` yazmaz (`-race`).
- **Zamanlama:** dört giriş durumu düz.

## 10. Sonuçlar ve kabul edilen riskler

**Kazanılan:** istenen deneyim; giriş çözümleyicisi md.7'nin ≤ 1'ine döner; DoS, sayım, zamanlama ve
kalabalıklaştırma kanalları emekli; parola/sıfırlama/"her yerden çık" tek yerde; M7-07 doğal evinde.

**Ödenen:** üçüncü §4.5 istisnası (beş tenant-kapsamsız tablo, bir definer rolü, kapalı liste); iki
giriş kolu (contract'a dek); iki contract deploy'u; kayıt akışına e-posta doğrulaması.

**Kabul edilen riskler:**
- **Çalınan çerezin etki alanı büyür** (bulgu 14): bugün bir panel çerezi tek işletmeye açılır;
  M14'te kişinin bütün işletmelerine geçebilir. Geçiş `authenticated_at`'i taşır (yenilemez), ama
  oturumun sunucu tarafı mutlak ömrü yoktur (`cookie.go:57-75`) — backlog maddesi (mutlak ömür +
  hassas işlemlerde yeniden doğrulama).
- **Expand penceresinde** eski pod'un bağlı satıra parola yazması 500 alır (§5.1).
- **`identity_open_session` ve `identity_create_for_signup` sırra anahtarlı değildir** — güvenceleri Go
  tipinde ve fonksiyonun reddetme kurallarındadır.
- `tappa_app` `identity_id`'yi okur (oran kovası) — tarama testleriyle sınırlı.

## 11. Reddedilen fikirler

- "Aynı e-posta = aynı kişi, otomatik birleştir" — 00011 saldırısı.
- Hedef tenant'ı imzalı çerezde taşıyıp istek başına bağlam kurmak — `00011:24-31`.
- Tek oturum + istek başına "aktif tenant" — per-request yolu tenant-kapsamsız tabloya bağlar.
- Satır → kimlik eşitleyen tetikleyici — tenant A'nın yazmasını B'ye taşır.
- Operatörün kimlik bağlaması — ADR 0020.
- Holding üyeliğinden otomatik üyelik — ADR 0002 md.6, ADR 0027 `:881`.
- E-posta değiştirme M14 içinde — K-15.

## 12. Uygulama planı

[docs/plan/m14-tek-kimlik.md](../plan/m14-tek-kimlik.md).

## 13. Kararlar (2026-10-21)

Kullanıcı 2026-10-21'de K-1 … K-14'ün hepsinde (a) seçti; aynı gün denetimlerden sonra **K-13'ü (b)'ye
çevirdi**. Aşağıda her karar, seçilen seçenek ve denetim düzeltmelerinin anlamına etkisi.

| # | Karar | Seçilen | Düzeltmenin etkisi |
|---|---|---|---|
| K-1 | E-posta global tekil (kimlik başına) | (a) | — |
| K-2 | Birleştirme yalnız iki kimlik bilgisinin kanıtıyla | (a) | **Anlam sıkılaştı** (bulgu 9): kanıt artık veritabanında **iki canlı oturum**; e-posta eşitliği yalnız savunma katmanı |
| K-3 | Bağlanmamış satırlar için son tarih, 60 gün | (a) | **Anlam değişti** (bulgu 20): 60 gün bayrağın açıldığı gün değil, **M14-14 ve M14-18 canlıya çıktığı gün** başlar; ek ön koşul: tek owner'ı bağlanmamış tenant = 0 (bulgu 13) |
| K-4 | Girişte son kullanılan işletme, seçici yok | (a) | **Anlam daraldı** (bulgu 10): "seçici yok" kimlik yoluna aittir; eski kolda birden çok satır doğrularsa seçici contract'a dek kalır |
| K-5 | Kabukta ad + "Switch business" bağlantısı, sayfada liste | (a) | — |
| K-6 | Devre dışı bırakma yalnız üyelik başına | (a) | **Anlam değişti** (bulgu 24): "kimlik kilidi yalnız sistemin" ifadesi kalktı — M14'te kimlik kilidi **hiç yok** (`status` sütunu yok) |
| K-7 | Var olan kimliği yalnız owner, davetle; davetli kendi parolasıyla kabul eder | (a) | **Sıkılaştı** (bulgu 12): davet e-postası kimliğin e-postasına eşit ve kimlik doğrulanmış olmalı; kabul oturum vermez |
| K-8 | Rol işletme başına | (a) | — |
| K-9 | Kayıt e-posta doğrulamasıyla biter; var olan adres aynı ekranı görür | (a) | **Genişledi** (bulgu 7, 11): tıklamadan önce hiçbir şey doğmaz; M14-17 bayrağı açmanın ön koşulu |
| K-10 | Askıdaki işletme değiştiricide işaretli, geçiş serbest | (a) | — |
| K-11 | Sahiplik devri ve kaldırma M14'te | (a) | — |
| K-12 | Operatör değişmez | (a) | **Genişledi** (bulgu 25): gelecekteki `op_disable_tenant_admins` yalnız üyelik durumuna dokunur |
| K-13 | §4.5 sınıflandırması | **(b) — üçüncü açık istisna** | **Değişti (2026-10-21, denetimden sonra).** Gerekçe: güvenlik denetimi, yazan ve tenant sınırını geçen fonksiyonları "md.7 çözümleyicisi" saymanın md.7'nin "salt okuma, bağlam üreten" tanımını genişlettiğini ve sınırı görünmez kıldığını gösterdi; kullanıcı istisnanın **adıyla** yazılmasını seçti. md.7'de yalnız salt okuma çözümleyiciler kalır; yazanlar §6.1'deki kapalı listedir |
| K-14 | `admin_users.email` ayna, `full_name` üyelikte | (a) | **Sıkılaştı** (bulgu 21): `tappa_app`'ın `UPDATE (email)`'i expand'de kapanır |
| K-15 | **Yeni (bulgu 12):** e-posta değiştirme | M14'ün **dışında** (öneri) | Var olan kimliğin adresini değiştirmek mevcut parola + yeni adresin doğrulanması + eski adrese bildirim + çakışmada aynı "gelen kutunu kontrol et" + aynanın bütün işletmelerde definer ile güncellenmesini ister; kendi kartı ve kararıyla gelir. Kullanıcıya M14-01'de sorulur |
