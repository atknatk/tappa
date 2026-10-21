# ADR 0031 — Tek kimlik, çok işletme: panel yöneticisi için global kimlik + işletme başına üyelik

- **Durum:** Önerilen — kullanıcı kararı bekliyor. Kod, migration ve commit **yok**; bu ADR ve
  [docs/plan/m14-tek-kimlik.md](../plan/m14-tek-kimlik.md) yalnız plandır. Açık kararlar §13'te
  numaralıdır; her birinin önerisi yazılıdır. Kabul edilince durum satırı tarihle güncellenir.
- **Tarih:** 2026-10-21
- **Karar veren:** kullanıcı (ürün sahibi) — 2026-10-21: *"Hesapları birleştir (tek kimlik)"*: bir
  kişi = tek giriş kimliği, birden çok işletmeye (tenant) üye olabilir; ör. Kebab Factory ile Kebab
  Manufacturing arasında (aynı holding, ADR 0027) **çıkış yapıp yeniden girmeden** geçebilir.
  Kullanıcı işe başlamadan önce ayrıntılı plan istedi.
- **İlgili:** [ADR 0002](0002-tenant-baglami-ve-rls.md) md.2/3/6/7 ·
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
- **Etkilenecek (uygulanırsa):** yeni goose migration'ları (bugün son `00063`) ·
  `scripts/db-init/01-roles.sql` + canlı küme runbook'u (yeni definer rolü) ·
  `scripts/redline-check.sh` muafiyet satırları (migration içinde) · `internal/adminauth` ·
  `internal/handler/adminlogin.go`, `logincontext.go`, `adminreset*.go`, `accountpassword.go`,
  `adminratelimit.go` · `internal/domain/signup` · `web/templates/pages/admin.templ` (kabuk,
  seçici) · CLAUDE.md §4.5 ve §6 metni · ADR 0002 / 0013 / 0015 / 0025 notları.

Atıflar (`yol:satır`) bu ADR yazılırken `main` `0e7c158`'e göredir.

---

## 1. Problem

Bugün bir panel yöneticisinin kimliği **bir işletmeye gömülüdür**. Aynı kişinin iki işletmesi varsa
iki ayrı `admin_users` satırı, iki ayrı parola, iki ayrı sıfırlama linki ve iki ayrı oturumu vardır;
işletme değiştirmek için çıkış → giriş → "Which business?" seçicisi gerekir. Seçicinin kendi metni
bunu söyler: *"you can sign out and switch at any time"* (`web/templates/pages/admin.templ:311`).
M12 tasarım panolarındaki kabuk-içi değiştirici (S1, S7) bu yüzden D1 ile **kaldırıldı**
(`docs/design/panel-redesign/screens/account-auth.md:29`, `:35`; backlog
`docs/backlog.md:294`).

Kullanıcı kararı bunu tersine çevirir: kimlik kişiye aittir, işletme bir **üyeliktir**.

## 2. Bugünkü model — kanıtla

| Olgu | Kanıt |
|---|---|
| `admin_users` tenant'a bağlı; e-posta **tenant başına** tekil (`UNIQUE (tenant_id, email)`), global değil | `db/migrations/00006_create_admin_users.sql:47-97` |
| `admin_sessions` ve `password_resets` yöneticiye **bileşik FK** `(admin_user_id, tenant_id)` ile bağlı → oturum tek tenant'a damgalı | `00006:123-146`, `:173-196` |
| Giriş tek URL; e-posta **globalde** çözülür (`resolve_admin_by_email`, SECURITY DEFINER, `tappa_resolver`), N aday döner; kullanıcı kararı 2026-08-02 "global çözümleme + tenant seçici", gerekçe: KF ve KM aynı kişinin | `db/migrations/00011_add_admin_resolution.sql:9-31`, `:315-350`; ADR 0002 `:80-87` |
| Çözümleyici **incumbent-first** (`ORDER BY a.created_at, a.tenant_id`), LIMIT yok | `db/migrations/00017_add_vat_verification_and_incumbent_first_resolution.sql:423-474` |
| Bir giriş en çok `MaxCandidates = 8` adayı bcrypt'ler; her giriş 8'e **dolgulanır**; seçici `PickerCap = 7` | `internal/adminauth/manager.go:118`, `:156`, `:400-480` |
| Saldırı (00011 YÜKÜMLÜLÜK 5): saldırgan herkese açık kayıtla kendi tenant'ına **kurbanın e-postasını** yazar; parola kendi satırında doğrular; seçici kurbanın işletmesini sunsaydı kurbanın tenant'ında `role=owner` oturum açılırdı. Kapanış: seçici yalnız **parolası doğrulanan** adaylardan kurulur (`Verified` tipi) | `internal/adminauth/manager.go:285-292`; `00011:131-160` |
| Seçici seti imzalı kısa ömürlü çerezle taşınır (`tappa_admin_choice`, 5 dk, HMAC); POST'ta set üyeliği yeniden kanıtlanır (`selectVerified`) | `internal/handler/logincontext.go:20-60`, `:134`, `:205`, `:623` |
| Panel çerezi `tappa_admin_session`, `Path=/admin`, 12 saat ipucu; **sunucu tarafı son kullanma yok** | `internal/adminauth/cookie.go:27`, `:48`, `:57-75` |
| Her istek: bağlamsız `resolve_admin_session_by_token_hash` → `WithTenant(rs.TenantID)` içinde `TouchAdminSession` (yönetici `active` mi, askı sütunları) | `internal/adminauth/manager.go:851-905`; `db/queries/admins.sql:125-166`; `internal/httpx/adminidentity.go:128-174` |
| Tenant asla istemci girdisi değildir, oturumun **çıktısıdır** | `internal/httpx/adminidentity.go:68-81`; `00011:26-31` |
| Kendi parolasını değiştirme **tek satırı** (tek tenant) yazar, yalnız o tenant'taki diğer oturumları iptal eder | `internal/adminauth/changepassword.go:57-121` |
| Sıfırlama talebi: bir adres N satıra çözülür → **satır başına bir link** (en çok 8), her link yalnız kendi satırını yazar (ADR 0015 "tek geçişlik yetki") | `internal/adminauth/resetrequest.go:50-68`, `:157-224`; `internal/handler/adminreset.go:601-603`; ADR 0015 `:32-45` |
| `admin_users` satırı yaratan **tek** üretim yolu kayıt sihirbazı (`role='owner'`); `manager` üründe ulaşılamaz; yönetici daveti M7-07 / EM-12 **yapılmadı** | `internal/domain/signup/signup.go:734-742`; `docs/plan/m7-portal.md:1509-1591`; `docs/plan/state.md:1653` |
| Yöneticiyi listeleme / devre dışı bırakma için **hiçbir panel yolu yok**; `status='disabled'` yalnız elle SQL ile | `db/queries/admins.sql` (status güncelleyen sorgu yok); `RevokeAdminSessionsForAdmin`'in tek çağıranı `internal/adminauth/reset.go:647` |
| `admin_users.id` aktör olarak: bileşik FK'li `api_keys.created_by`, `billing_periods.closed_by`, `tenant_branding.updated_by`; FK'siz `audit_log.actor_id`, `transactions.entered_by`, `transaction_reviews.reviewer_id`, `policies.created_by` | `00051:80-89`; `00016:551-572`; `00028:69-128`; `00005:115-118`, `:212-215`, `:264-267`; `00007:60`, `:122` |
| Kabukta işletme adı **yalnız markalı** tenant'ta görünür; `PanelChrome`'da tenant adı alanı yok | `web/templates/pages/admin.templ:453-504`, `:620-646`; `internal/handler/panelbrand.go:108-116` |
| Askı: yöneticiler girer ve okur; yazmalar 403; `login`, `login/choose`, `logout` kapalı istisna listesinde | ADR 0025 `:45-50`, `:374-387`, `:509-514`; askı `TouchAdminSession`'dan okunur `:322-325` |
| Holding hiçbir panel kimliği vermez: holding dışı tenant hiçbir yoldan erişemez; MVP'de süper-admin / çapraz-tenant erişim yok | ADR 0027 `:881`, `:1146`; ADR 0002 md.6 `:286-288` |
| Operatör kimliği **ayrıdır** (ayrı tablo `platform_admins` global tekil e-posta, ayrı host, ayrı çerez, TOTP); `admin_users.role`'e üçüncü değer girmez | ADR 0020 `:68-93`, `:144-146`, `:1094-1107` |
| Hiçbir `op_*` tenant ya da yönetici yaratmaz/değiştirmez; operatörün `admin_users` yetkisi `SELECT (tenant_id, email, status)` | `db/migrations/00029_read_tenants_from_the_operator.sql:159-169` |
| Çalışan oturumları (`internal/session`, `sessions`, `tappa_session` çerezi) bu işin **dışında** | `internal/session/cookie.go:33`; `00006` başlığı `:5-15` |

Ürün verisinin ölçülmesi gereken bir kısmı vardır ve **ölçülmedi** (canlı veritabanına erişim bu
planın kapsamında değil): canlıda kaç e-posta birden fazla `admin_users` satırına çözülüyor, kaçında
parolalar aynı. `resetrequest.go:50-57`'nin *"1,675 adres"* sayısı geliştirme veritabanınındır,
üretimin değil. Bu sayı M14-02'nin işidir ve §8'in geçiş planı onu ön koşul sayar.

## 3. Hedef ve sınırlar

**Hedef.** (1) Bir kişi bir e-posta + bir parola ile girer. (2) Üye olduğu her işletme panelde
listelenir; tek tıkla, parola sormadan geçer. (3) Parola, sıfırlama ve "her yerden çık" kişi
başınadır. (4) Rol, durum ve kayıtlar **işletme başına** kalır.

**Değişmeyenler (sınır).**
- §4.5: her tenant verisi RLS altında kalır; `tappa_app` NOBYPASSRLS ve tablo sahibi değil;
  sorgularda açık `tenant_id`. Bir istek her an **tek** tenant bağlamında koşar.
- Tenant istemci girdisi olmaz (`adminidentity.go:68-81`). Geçişte hedef tenant formdan gelir ama
  **yetki** oturumun sırrından ve hedefteki üyelik satırından türetilir; form yalnız *hangisini*
  seçer, *izin* vermez.
- Operatör kimliği ayrı kalır (ADR 0020); M14 operatöre kimlik gücü vermez.
- Holding üyeliği kimlik ya da üyelik **ima etmez** (ADR 0027).
- Çalışan kimliği (`employees` / `sessions`) ve tap yolu etkilenmez.

## 4. Değerlendirilen seçenekler

### A — Yeni `identities` + yeni `memberships`; `admin_users` emekli

Temiz şema, ama `admin_users.id`'ye yaslanan **dört bileşik FK** ve en az beş FK'siz aktör sütunu
(§2 son satırları) yeni üyelik kimliğine taşınmak zorunda. `transactions` ve `audit_log`
**değişmez** (§4.3, §4.6): geçmiş satırların `entered_by` / `actor_id` değerleri yeniden
yazılamaz, yani eski id'ler sonsuza dek eski tabloyu işaret eder. **Reddedildi:** maliyet büyük,
değişmezlik kuralıyla çatışır, kazancı yalnız isimlendirme.

### A′ — Yeni tenant-kapsamsız `admin_identities`; `admin_users` **üyelik olarak kalır** (`identity_id` eklenir) — **ÖNERİLEN**

`admin_users` satırı = *"bu kişi bu işletmede, şu rolle, şu durumda"*. `id`'si değişmez, bütün
aktör referansları ve FK'ler aynen geçerli; RLS politikası aynen geçerli. Yeni olan yalnız kişiye
ait şeyler: e-posta (global tekil), parola digest'i, e-posta doğrulaması, kilit durumu. Bunlar
tenant'a ait değildir, dolayısıyla tenant-kapsamsız bir tabloda durur ve uygulamaya **yalnız dar
SECURITY DEFINER fonksiyonlarla** açılır (§5).

### B — `admin_users` olduğu gibi + `admin_identity_links` (satırları birbirine bağlayan tablo)

Her satır kendi parolasını korur; bağ, iki parolanın kanıtıyla kurulur; geçiş bağlı satırlar
arasında serbest. Şema değişikliği küçük. **Reddedildi:** "tek kimlik" vermez — iki parola, iki
sıfırlama linki, iki "parolanız değişti" e-postası kalır; bir parolayı değiştiren kişi öbürünü
eski bırakır; sıfırlama hangi satırı yazar sorusu her akışta yeniden doğar. Üstelik bağ tablosunun
kendisi tenant-kapsamsızdır, yani §4.5 bedelini A′ kadar öder, kazancın yarısını verir.

### C — Şemasız: girişte doğrulanan seti oturum boyu taşı

Bugünkü seçicinin imzalı setini (`logincontext.go`) oturum ömrüne uzat; kabukta yalnız o set
arasında geç. Migration yok. **Reddedildi (ama acil durum seçeneği olarak not edildi):** yalnız
**aynı parolayı** taşıyan satırlar arasında çalışır; parola, sıfırlama ve oturumlar satır başına
kalır; `MaxCandidates`/dolgu/`PickerCap` mekanizmalarının hiçbiri emekli olmaz. Kullanıcının
kararı olan "tek kimlik"i değil, yalnız "çıkmadan geç"i verir. Kullanıcı M14'ü ertelemek isterse
bir-iki kartlık ara çözüm olarak açılabilir.

### D — Tek `admin_users` satırı, işletmeler bir dizi sütununda

RLS'in "satır tek tenant'a aittir" varsayımını kırar. **Reddedildi.**

## 5. Önerilen tasarım (A′)

### 5.1 Şema

**`admin_identities`** (tenant-kapsamsız; yeni):

| Sütun | Not |
|---|---|
| `id uuid PK` | kişinin kimliği |
| `email citext NOT NULL UNIQUE` | **global tekil** (karar K-1). citext eşitliği `OPERATOR(public.=)` ile — 00011'in search_path tuzağı (ADR 0002 `:125-140`) |
| `password_hash text NOT NULL` | 00018'in bcrypt CHECK'i aynen (`admin_users_password_hash_is_bcrypt` deseni, ADR 0014) |
| `email_verified_at timestamptz NULL` | sıfırlama / davet / doğrulama linkine tıklanınca dolar (K-9) |
| `status text CHECK (status IN ('active','locked'))` | `locked` yalnız sistemin (§5.8) |
| `last_used_tenant uuid NULL` | girişte iniş (K-4). **Adı `tenant_id` değildir** — CLAUDE.md §6: tenant verisi olmayan sütun o adı taşımaz, R5 ve RLS testleri tabloyu o addan tenant tablosu sayar |
| `created_at`, `last_login_at` | `created_at` uygulamaya yazılamaz (00017'nin incumbent dersi) |

Tenant-kapsamsız tablo kuralları (CLAUDE.md §6; ADR 0021 `:234-254`, `:1249-1253`; ADR 0016
`:102-118`): Up bölümünde `-- redline: no-tenant-scope(admin_identities) — <gerekçe>` satırı
(R5 her koşuda WARN basar, muafiyet sessiz kalmaz — `scripts/redline-check.sh:511-517`,
`:1111-1135`) · `tappa_app`'tan **açık** `REVOKE ALL` (tablo ve sıra) · `ENABLE` **ve** `FORCE ROW
LEVEL SECURITY` birlikte (politika yok → definer dışında herkese kapalı;
`scripts/pg-restore-verify.sh:147-157` sayıları karşılaştırır) · `has_any_column_privilege` ile
"yetki yok" kanıtı (ADR 0021 `:249-254`).

Aynı kurallarla iki küçük tenant-kapsamsız tablo daha: **`admin_identity_resets`** (kimlik başına
sıfırlama token'ının hash'i, `expires_at`, `used_at`, `cancelled_at`; 00019'un biçim ve TTL
CHECK'leri) ve **`admin_identity_tokens`** (e-posta doğrulama / e-posta değişikliği / üyelik daveti
kabulü — amaç sütunlu, tek kullanımlık, hash saklanır). İkincisi K-9 ve M14-15'e bağlıdır; kabul
edilmezse yalnız sıfırlama tablosu kalır.

**`admin_users`** (tenant-kapsamlı; mevcut tabloya ekleme):
- `identity_id uuid NULL REFERENCES admin_identities(id) ON DELETE RESTRICT`
- `UNIQUE (tenant_id, identity_id)` — bir kişinin bir işletmede tek üyeliği.
- 🔴 **`tappa_app` `identity_id`'ye INSERT de UPDATE de edemez** (00017'nin sütun düzeyi GRANT
  kalıbı). Sebep tam olarak `manager.go:285-292`'deki saldırının ikizidir: RLS, bir tenant'ın kendi
  satırına **istediği** `identity_id`'yi yazmasına izin verirdi — saldırgan kurbanın kimliğini kendi
  işletmesine "üye" yapardı. Erişim kazanmaz (kurbanın tenant'ına satır yazamaz) ama kurbanın
  değiştiricisine saldırganın işletmesi düşerdi (oltalama yüzeyi). Bu sütunu yalnız definer
  fonksiyonları yazar ve her yazışın bir **rıza kanıtı** vardır (§5.6).
- `email`: geçiş süresince ayna (kimliğin e-postası); contract'ta `tappa_app`'ın `UPDATE (email)`
  yetkisi kapanır ve sütunu yalnız kimlik fonksiyonu yazar. Tenant'ın kendi ekibini listelerken
  RLS içinde okuyabilmesi için tutulur (karar K-14 — ayna mı, definer okuması mı).
- `password_hash`: **dual-write** süresince kimliğin digest'inin kopyası (geri dönüş, §8);
  contract'ta NULL'lanabilir olur ve okunmaz. Bağlanmamış eski satırlarda kendi parolasıdır.
- `full_name`, `role`, `status`: **üyeliğe** ait kalır (rol işletme başına — K-8; görünen ad işletme
  başına — K-14).

`admin_sessions`: **şema değişmez.** Oturum hâlâ tek üyeliğe, tek tenant'a damgalıdır.

### 5.2 Roller ve definer fonksiyonları

Yeni rol **`tappa_identitydefiner`**: NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesi yok, yalnız sütun
düzeyi GRANT; `01-roles.sql`'de yaratılır (migration'da değil — R5b, ADR 0021 `:759-763`) ve canlı
kümeye runbook ile uygulanır (`tappa_holdingdefiner` emsali, `scripts/db-init/01-roles.sql:133-154`).
Salt okuma çözümleyicisi `tappa_resolver`'ın kalır (ADR 0002 md.7 sınıfı).

Her fonksiyon: `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, şema-nitelikli gövde,
dinamik SQL yok, sabit sütun listesi, `REVOKE ALL … FROM PUBLIC` + `GRANT EXECUTE … TO tappa_app`
yalnız. Katalog testi: her `prosecdef` fonksiyonun sahibi izinli küme {`tappa_resolver`,
`tappa_opdefiner`, `tappa_holdingdefiner`, `tappa_identitydefiner`} içinde ve `rolsuper=f` (ADR 0021
`:789-812` testinin kümesi genişler).

| Fonksiyon | Anahtar (girdi) | Çıktı | Ne yapar |
|---|---|---|---|
| `resolve_identity_by_email(p_email)` | e-posta | ≤ 1 satır `(id, password_hash, status)` | Girişin md.7 çözümleyicisi. **Global UNIQUE sayesinde ADR 0002 md.7 (ii) "≤ 1 satır" özelliği giriş için geri gelir** — bugün e-posta istisnadır (`:316-322`) |
| `identity_open_session(p_identity_id, p_tenant, p_token_hash)` | kimlik (parolası uygulamada doğrulanmış) | ≤ 1 `(session_id, admin_user_id, tenant_id)` | İstenen ya da `last_used_tenant` ya da en eski **aktif** üyelikte `admin_sessions` satırı açar; `last_login_at`, `last_used_tenant` damgalar; o tenant'ın `audit_log`'una giriş satırı |
| `identity_memberships(p_token_hash)` | **canlı oturumun** token hash'i | ≤ 50 `(tenant_id, tenant_name, role, suspended, is_current)` | Değiştirici listesi. Yalnız aktif üyelikler; LIMIT (ADR 0021 (iii) emsali) |
| `identity_switch_session(p_token_hash, p_target_tenant, p_new_token_hash)` | canlı oturum + hedef | ≤ 1 `(session_id, admin_user_id, tenant_id)` | §5.4 |
| `identity_revoke_sessions(p_token_hash, p_keep_current)` | canlı oturum | iptal sayısı | "Her yerden çık"; parola değişiminde öbürlerini iptal |
| `identity_password_for_session(p_token_hash)` | canlı oturum | ≤ 1 digest | Hesap sayfasında mevcut parolayı doğrulamak için (uygulama bcrypt'ler) |
| `identity_set_password(p_token_hash, p_new_hash)` | canlı oturum | — | Digest'i yazar, **dual-write** süresince bağlı `admin_users.password_hash`'lere de; öbür oturumları bütün işletmelerde iptal eder |
| `resolve_identity_reset_by_token_hash` / `identity_consume_reset` | sıfırlama token hash'i | — | §5.7 |
| `identity_link_legacy(p_token_hash, p_admin_user_id, p_tenant)` | canlı oturum + uygulamada doğrulanmış eski satır | — | §5.6 |
| `identity_accept_invite(...)` | davet token hash'i (+ mevcut kimlikse canlı oturum) | — | M14-15, K-7 |

**`identity_open_session`'ın anahtarı bir sır değildir** (kimlik id'si), ve bu bugünkü `Issue`'nun
durumunun aynısıdır: veritabanı parolanın doğrulandığını göremez (00011 `:131-139`). Bağ, bugünkü
gibi **Go tipinde** tutulur: fonksiyonu yalnız `adminauth`'un parola karşılaştırmasından çıkan
tipten (bugünkü `Verified`'ın kimlik karşılığı) çağıran bir yol vardır; yazılı testle sabitlenir.
Bunun dışındaki her fonksiyon ya bir e-postaya (çözümleyici) ya bir **sırra** (oturum / sıfırlama /
davet token hash'i) anahtarlıdır — ADR 0002 md.7'nin *"çözümleme saldırganın sahip olması gereken
bir sırra anahtarlıdır"* ilkesi (`00011:26-31`).

### 5.3 İstek başına yol — değişmez

Çerez → `resolve_admin_session_by_token_hash` → `WithTenant(rs.TenantID)` → `TouchAdminSession`
(üyelik `active` mi, oturum iptal mi, askı sütunları). `admin_identities`'e **istek başına okuma
yok**. Kimliğin kilitlenmesi (§5.8) ve parola değişimi oturumları **iptal ederek** etki eder, yani
per-request yol kimliği bilmek zorunda değildir. Tek ek: `TouchAdminSession`'ın `RETURNING`'ine
`tenants.name` eklenir (zaten `tenants`'ı birleştiriyor, `admins.sql:155-166`) — kabuk işletme
adını **ek okuma olmadan** gösterir.

Kazanç: ADR 0025'in askı kapısı, `ProtectWriting` zinciri, `RequireAdmin`, ~50 `AdminOf` çağrı yeri
ve rol kapıları **dokunulmadan** çalışır. Deploy sırasında var olan oturumlar yaşar (§8).

### 5.4 Geçiş (switch)

`POST /admin/switch` (form: `tenant_id`, `csrf`). Zincir: `floodGate → sameOriginGate →
requireAdmin → sessionGate` + senkronizör token. `suspensionGate` **uygulanmaz**: geçiş bir
tenant yazması değildir, ADR 0025'in kapalı istisna listesi (`:374-387`) bir rota büyür (ADR 0025'e
tarihli not). Akış, tek definer çağrısı ve tek transaction:

1. Mevcut oturum canlı (`revoked_at IS NULL`), üyeliği `active`, kimliği `active`.
2. Hedef tenant'ta **aynı `identity_id`**'li ve `active` bir `admin_users` satırı var. Yoksa 0
   satır → uygulama tek tip bir "bu işletmeye geçilemiyor" sayfası gösterir (hedefin var olup
   olmadığını söylemez).
3. Hedefte yeni `admin_sessions` satırı (uygulamanın ürettiği yeni token'ın hash'i) — **token her
   geçişte döner** (oturum sabitleme hijyeni; bugünkü `Issue` deseni).
4. Eski oturum `revoked_at` (00011'in monotonik tetikleyicisi korunur).
5. `last_used_tenant` güncellenir.
6. **İki audit satırı, iki tenant'ta, birbirini anmadan:** A'nın `audit_log`'una "oturum geçişle
   kapandı" (`session_id`), B'ninkine "oturum geçişle açıldı" (`session_id`, `via=switch`).
   🔴 A'nın satırı B'nin id'sini ya da adını, B'ninki A'nınkini **taşımaz**: A'nın sahibi kendi
   denetim günlüğünü okur ve bu kişinin başka hangi işletmeye üye olduğunu oradan öğrenmemelidir
   (§4.5'in ruhu; ADR 0027 `:915-918`'in "e-posta/GPS/IP asla paylaşılmaz" çizgisiyle aynı yön).

Hedef askıdaysa geçiş serbesttir (askıda giriş ve okuma açık — ADR 0025 `:509-514`); panel o
işletmede salt okunur açılır (K-10). Oran sınırı: oturum başına `adminSessionLimit` sınıfında ayrı
bir kova (bcrypt yok, ucuz; amaç döngüyle audit şişirmeyi kesmek).

### 5.5 Giriş

Bayrak açıkken (§8): e-posta → `resolve_identity_by_email` (≤ 1) → **tek** bcrypt (bilinmeyen
e-postada sahte digest'e karşı tek karşılaştırma: zamanlama tek tip kalır — 00011 YÜKÜMLÜLÜK 1–2)
→ `identity_open_session` → `/admin`. İniş: `last_used_tenant` hâlâ aktif üyelikse orası, değilse
en eski aktif üyelik (K-4). Aktif üyelik yoksa tek tip başarısızlık (bugünkü "disabled admin" ile
aynı yanıt).

**Geçiş dönemi (expand → contract arası) eski kol:** aynı e-postaya bağlanmamış eski satırlar
(`identity_id IS NULL`) varsa onlar da bugünkü gibi (en çok 8, incumbent-first, dolgulu)
karşılaştırılır. Yazılan parola hem kimliği hem bir eski satırı doğrularsa bu, **iki kimlik
bilgisinin birlikte kanıtıdır** → eski satır kimliğe bağlanır (`identity_link_legacy`, o tenant'ın
audit'ine "linked") ve kişi hiçbir şey yapmadan ikinci işletmesini değiştiricide görür. Yalnız eski
satır doğrularsa kişi bugünkü gibi o işletmeye **kimliksiz** girer; hesap sayfası "bu işletmeyi
hesabına bağla" der (§5.6). Bu kol contract'ta kalkar ve onunla birlikte `MaxCandidates`, dolgu,
`PickerCap`, `logincontext.go`'nun imzalı seti ve `signInBlocked` emekli olur — ADR 0013'ün sayılı
kanalları (`:271-323`) kapanır, çünkü bir e-posta artık tek kimliktir.

`adminLoginWorkLimit` (120/10 dk, `adminratelimit.go:737`) "her giriş 8 karşılaştırma" üzerine
boyutlandı; contract'tan sonra yeniden ölçülür, gevşetilmesi ayrı bir karardır (planda yok).

### 5.6 Birleştirme (eski çift hesaplar)

**Kural: iki satır yalnız kişi her ikisinin kimlik bilgisini kanıtlarsa birleşir** (K-2).
"Aynı e-posta" kanıt **değildir** — bütün `manager.go:285-292` / 00011 saldırısı tam olarak
budur, ve kayıt sihirbazı herkese açık, e-posta doğrulaması yok (ADR 0013 `:304-316`). Kanıt
yolları:

1. **Girişte otomatik:** aynı parola her iki satırı doğrular (§5.5). Ürünün tasarım ortakları için
   beklenen yol budur (ADR 0013 `:98`: *"aynı kişinin iki işletmesi genelde aynı parolayı taşır"*).
2. **Hesap sayfasında elle:** kimlikle girilmiş oturum + eski satırın parolası formda → uygulama
   eski satırın digest'ini (aynı e-postaya çözülen bağlanmamış satırlar, en çok 8, dolgulu)
   karşılaştırır → doğrulanan satırlar bağlanır. Ters yön (eski satırla girilmiş oturum + kimliğin
   parolası) aynı fonksiyonu çağırır.
3. **Operatör birleştiremez** (K-12). Operatör parola görmez, ADR 0020'nin ayrılığı ve
   *"impersonation yok"* (`:1094-1095`) kuralı onu kimlik otoritesi yapmayı dışlar; sosyal
   mühendislik yüzeyi açardı.

Bağlanan satırın kendi `password_hash`'i contract'ta okunmaz olur; satırın `id`'si, rolü, geçmişi
ve oturumları aynen kalır.

### 5.7 Parola sıfırlama — kimlik başına

Talep: e-posta → kimlik (≤ 1) → **tek** link (bugün satır başına en çok 8 —
`resetrequest.go:157-224`). Tüketim (ADR 0015'in tek geçişlik yetkisi korunur, *yetkinin nesnesi*
satırdan kimliğe geçer): kimliğin digest'ini bir kez yazar, `email_verified_at`'i doldurur
(adrese sahiplik kanıtlandı), kimliğin **bütün işletmelerdeki** oturumlarını iptal eder, oturum
vermez. ADR 0022'nin alıcı (§4), sızıntı (§3), log (§10) ve oran (§9) kuralları aynen. Geçiş
döneminde bağlanmamış eski satırlar bugünkü satır-başı sıfırlamayı korur.

🔴 Sıfırlama eski satırları **bağlamaz**: e-postaya sahiplik, o adresle kaydedilmiş her satırın
sahibi olmayı kanıtlamaz (saldırganın ektiği satır da o adresi taşır).

### 5.8 Devre dışı bırakma, kaldırma, sahiplik

- **Üyelik başına** (K-6): bir işletmenin sahibi, o işletmedeki bir yöneticiyi `status='disabled'`
  yapar → yalnız o tenant'taki oturumlar düşer (`TouchAdminSession` zaten `active` arar,
  `admins.sql:125-166`). Kişinin öbür işletmeleri etkilenmez — tenant A'nın kararı tenant B'ye
  ulaşamaz (§4.5).
- **Kimlik kilidi** (`locked`) yalnız sistemin: sıfırlama tüketilirken değil, gelecekte bir
  ihlal/kilit politikası için ayrılmış durum. Kilit → bütün oturumlar iptal. Hiçbir tenant ve
  operatör bunu tetikleyemez (K-6, K-12).
- **Son owner değişmezi:** bir işletmenin en az bir `active` owner'ı kalır; owner kendini
  düşüremez, devir önce yeni owner atar sonra eskisini indirir (M14-16; K-11).
- **Satır silinmez** (§4.6): kaldırma = `disabled`.

### 5.9 Davet ve "başka işletmeye ekleme"

M14-15, M7-07 / EM-12'yi devralır. Sahip bir e-postaya **manager** daveti gönderir (owner-only,
policy motorundan; M7-07'nin `deny sid=default` tuzağı `m7-portal.md:1570-1573`). Davet linki
davetlinin kendi adresine gider (ADR 0022). Kabul:
- adres bir kimliğe aitse → kişi **kendi parolasıyla** girer ve kabul eder → `admin_users` satırı
  `identity_id` ile o tenant'ta doğar;
- değilse → kişi parola belirler → yeni kimlik (`email_verified_at` dolu: linke tıkladı).

Hiçbir yol rızasız üyelik yaratmaz (K-7). Holding üyeliği davetin yerine geçmez (ADR 0027).

### 5.10 Kayıt sihirbazı

Yeni e-posta → yeni kimlik + owner üyeliği (aynı transaction). **Var olan** e-posta → global
tekillik ikinci kimliği reddeder; üç yol var (K-9):
(a) **önerilen:** her kayıt e-posta doğrulamasıyla biter (EM-11 M14'e katılır); var olan adresle
kayıt da aynı "gelen kutunu kontrol et" ekranını görür, adrese giden link kişiyi **kendi
parolasıyla** girip yeni işletmeyi sahiplenmeye götürür; sahiplenilmeyen tenant'ın owner üyeliği
hiç doğmaz → numaralandırma kahini yok;
(b) "bu e-posta kayıtlı, giriş yap" — sayılı bir kahin (ADR 0013'ün `signInBlocked`'ı gibi);
(c) sessizce var olan kimliğe bağlamak — **reddedilir** (saldırgan kurbanın değiştiricisine işletme
ekler).

### 5.11 API anahtarları, webhook'lar, aktör id'leri

Değişmez. `api_keys.created_by` üyeliğin (`admin_users.id`) bileşik FK'sidir (`00051:80-89`) ve
öyle kalır; anahtar tenant'ındır (ADR 0028). Bir üyelik `disabled` olunca anahtarları çalışmaya
devam eder — bugünkü davranış (ADR 0028 bu durumu ele almıyor; çözümleyici yaratıcının durumuna
bakmaz). Bu M14'ün kapsamında değildir; backlog'a not olarak düşülür. Webhook'ların aktörü yok
(`00048:186-199`). `audit_log.actor_id` üyelik id'si kalır → bir tenant'ın denetim günlüğü başka
tenant'taki üyelik id'sini hiç görmez.

### 5.12 Operatör

Değişmez (ADR 0020/0021). Operatörün `admin_users` okuması (`00029:159-169`) aynen; kimlik
tablolarına **hiçbir** `op_*` yetkisi verilmez (K-12 önerisi). Geçişin izlenmesi (bağlanmamış eski
satır sayısı) runbook ile `tappa_owner`'dan, yalnız sayı olarak, e-posta yazmadan yapılır.

### 5.13 Log ve gizlilik

CLAUDE.md §7: e-posta adresi, oturum/sıfırlama/davet token'ı log'a yazılmaz; log anahtarları ADR
0022 §10'un kapalı kümesinde kalır, kümeye `identity_id` eklenir. Audit `detail`'i e-posta taşımaz.

## 6. §4.5 nasıl korunur — özet

| Katman | Durum |
|---|---|
| Tenant verisi (`admin_users`, `admin_sessions`, tüm iş tabloları) | RLS + FORCE + açık `tenant_id`, **değişmez** |
| Kişi verisi (`admin_identities`, sıfırlama, token tabloları) | Tenant-kapsamsız; `tappa_app` hiçbir yetki taşımaz; yalnız `tappa_identitydefiner` / `tappa_resolver` sahipli dar fonksiyonlar |
| Tenant sınırını geçen tek hareket | **Geçiş**: bir sırra (canlı oturum) anahtarlı, hedefteki üyelik satırıyla yetkilendirilmiş, sonucu yeni bir **tek-tenant** oturum. Bir istek asla iki tenant'ta koşmaz |
| `tappa_app`'ın kimlik bağı yazması | Sütun düzeyinde **kapalı** (`identity_id`) |
| Çapraz sızıntı | Audit satırları karşı tenant'ı anmaz; değiştirici listesi yalnız kişinin **kendi** oturumuyla okunur |

**CLAUDE.md metni değişir (K-13).** §4.5 bugün *"bilinçli istisnalar iki tanedir"* der
(`op_*` ve holding); §6'nın tenant-kapsamsız tablo listesi yalnız operatörü ve holding'i sayar. Bu
ADR kimlik fonksiyonlarını **ADR 0002 md.7 sınıfında** (bağlam üreten, sırra anahtarlı
çözümleyici) konumlandırmayı önerir: geçiş bağlamı *aşmaz*, yeni bir bağlam *üretir*, tıpkı girişin
yaptığı gibi. Yine de bu bir yorumdur ve §4.5'e değer; kullanıcı ya bu sınıflandırmayı onaylar
(§6'ya `admin_identities` + iki tablo eklenir, §4.5 md.7 çözümleyicilerini anar) ya da bunu açıkça
**üçüncü istisna** olarak yazdırır. Her iki durumda CLAUDE.md değişikliği M14-01'in işidir ve
kullanıcı onayı ister.

## 7. `manager.go:285-292` saldırısı ne olur

| Aşama | Durum |
|---|---|
| Bugün | Açık sınıf, `Verified` tipi + imzalı set + `selectVerified` ile kapalı; `MaxCandidates` / dolgu / `PickerCap` ve incumbent-first yan bedeller (DoS, sayım ve zamanlama kanalları) |
| Expand + bayrak açık | Kimlik yolunda e-posta tek kimliğe çözülür: saldırganın ektiği satır **kimlik değildir**, bağlanmamış eski satırdır; eski kol bugünkü korumalarıyla sürer |
| Kayıt (K-9 a) | Var olan adresle yeni kimlik doğamaz; owner üyeliği yalnız adres sahibinin parolasıyla doğar |
| Contract | Eski kol kalkar; `admin_users.email` ve `password_hash` kimlik doğrulamasında okunmaz; `identity_id` uygulamaya yazılamaz → sınıf **yapısal olarak** kapanır. `MaxCandidates`, dolgu, `PickerCap`, `signInBlocked` ve ADR 0013'ün ön-kayıt kalabalıklaştırma limiti emekli olur |

Yorum `manager.go:285-292` contract kartında silinmez, **tarihsel** olarak işaretlenir ve bu ADR'ye
bağlanır.

## 8. Geçiş (expand → bayrak → contract)

1. **Expand deploy:** migration'lar (tablolar, sütun, roller runbook'la önceden, fonksiyonlar,
   backfill) + yeni binary, bayrak `kapalı`. Davranış aynı; **yazma yolları** (kayıt, parola
   değişimi, sıfırlama) kimliği de günceller (dual-write) ki bayrak her an açılıp kapanabilsin.
   migration Job'u rollout'tan önce koşar ve eski pod yeni şemaya karşı bir süre hizmet verir
   (`deploy/k8s/30-migrate-job.yaml:15-24`, `20-app.yaml:53-66`) → expand **yalnız eklemeli** olur;
   eski binary'nin kayıt INSERT'i `identity_id NULL` ile çalışmaya devam eder ve o satırlar tembel
   yolla (§5.5) bağlanır.
2. **Backfill** (idempotent, tek migration): her dolu e-posta grubu için — tek satırlı grup → kimlik
   (digest kopyalanır) + bağ; çok satırlı grup → **en eski** satırdan kimlik (incumbent-first, 00017
   ile aynı gerekçe: *"saldırganın yapamayacağı tek şey daha önce kaydolmaktır"*) + yalnız o satır
   bağlanır; öbürleri bağlanmamış eski satır kalır (§5.6). E-postasız satıra kimlik yok (bugün de
   giriş yapamaz, `00011:62-63`).
3. **Bayrak açık** (ConfigMap değişikliği, kod deploy'u değil): giriş, geçiş, değiştirici. Geri
   dönüş = bayrağı kapatmak; dual-write sayesinde eski yol doğru parolayı görür.
4. **Soak + ölçüm:** bağlanmamış eski satır sayısı haftalık (runbook, yalnız sayı).
5. **Contract deploy** (K-3'ün son tarihinden sonra): eski kol, `resolve_admin_by_email`, seçici
   blob'u, dolgu kalkar; `admin_users.password_hash` NULL'lanabilir ve okunmaz; `tappa_app`'ın
   `UPDATE (email)`'i kapanır. **Tek yönlü:** Down, şemayı geri getirir ama contract'tan sonra
   değişmiş parolaları eski satırlara geri yazamaz — ADR 0010'un "tek yönlü" dürüstlüğüyle yazılır.

Var olan oturumlar her adımda yaşar: `admin_sessions` hiç değişmez; bayrak açılınca yalnız
değiştirici görünür hale gelir (bağlı üyelikler için).

## 9. Testler (kabul kriterlerinin özü — adlar yapıcının)

- **RLS izolasyonu (filtresiz sonda):** A'nın bağlantısı, aynı kimliğin B'deki `admin_users`
  satırını görmez; `tappa_app` `admin_identities`, `admin_identity_resets`, `admin_identity_tokens`
  üzerinde `has_any_column_privilege = false`; `tappa_app` `identity_id` yazamaz (INSERT ve UPDATE
  `permission denied`).
- **Definer katalog testi:** sahip, `search_path`, PUBLIC EXECUTE yok, `rolsuper=f`, rolün üyesi yok.
- **Geçiş:** üyeliği olmayan hedef → 0 satır; `disabled` üyelik → 0; iptal edilmiş oturum → 0;
  başka kimliğin oturumu → 0; başarılı geçişte eski oturum iptal, yeni token, iki audit satırı ve
  satırların karşı tenant'ı anmadığı (metin taraması).
- **Mutasyon kanıtları** (her biri testin kırmızıya döndüğünü gösterir): geçişte kimlik eşitliği
  yüklemi silinince · `status='active'` silinince · eski oturum iptali silinince · `identity_id`
  sütun GRANT'ı açılınca · çözümleyiciden UNIQUE kaldırılınca · audit satırına hedef tenant eklenince.
- **Giriş:** bilinmeyen / yanlış parola / devre dışı üyelik / kilitli kimlik aynı yanıt ve tek tip
  zamanlama; eski kolda bugünkü saldırı testinin (`TestAuthenticate_RefusesTheCrossTenantBypass`)
  karşılığı kimlik yolunda yeniden koşar.
- **Eşzamanlılık:** aynı oturumla N paralel geçiş → tam olarak bir yeni canlı oturum (`-race`).

## 10. Sonuçlar

**Kazanılan:** kullanıcının istediği deneyim; giriş çözümleyicisi md.7'nin ≤ 1 özelliğine döner;
DoS, sayım, zamanlama ve kalabalıklaştırma kanallarının toplu emekliliği; parola/sıfırlama/"her
yerden çık" tek yerde; M7-07 yönetici daveti doğal bir evde.

**Ödenen:** üç yeni tenant-kapsamsız tablo ve bir definer rolü (CLAUDE.md §4.5/§6 metni değişir);
geçiş dönemi boyunca iki giriş kolu; contract tek yönlü; kayıt akışına e-posta doğrulaması (K-9 a
seçilirse onboarding'e bir adım).

## 11. Reddedilen fikirler (kısa)

- "Aynı e-posta = aynı kişi, otomatik birleştir" — 00011 saldırısının kendisi.
- Hedef tenant'ı imzalı çerezde taşıyıp istek başına ondan bağlam kurmak — `00011:24-31`'in
  reddettiği tasarım (imza *kimin* oturumu olduğunu kanıtlamaz).
- Tek oturum, istek başına "aktif tenant" sütunu güncelleyerek geçiş — per-request yolu
  tenant-kapsamsız bir tabloya bağlar; ADR 0025'in tek-ifade askı okuması bozulur. Oturumu
  yeniden vermek per-request yolu hiç değiştirmez.
- Operatörün kimlik bağlaması / kişileri birleştirmesi — ADR 0020 ayrılığı.
- Holding üyeliğinden otomatik üyelik — ADR 0027 `:881`, `:1146`.

## 12. Uygulama planı

[docs/plan/m14-tek-kimlik.md](../plan/m14-tek-kimlik.md) — fazlar P0–P7, kartlar M14-01 … M14-21.

## 13. Açık kararlar

Plan dosyasının *"Açık kararlar"* bölümündedir (K-1 … K-14); her birinin seçenekleri ve önerisi
oradadır. Bu ADR'nin §5'i önerilerin seçildiğini varsayar; farklı bir seçim ilgili alt bölümü
değiştirir ve ADR kabul edilmeden önce güncellenir.
