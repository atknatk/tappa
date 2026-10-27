# ADR 0031 — Tek kimlik, çok işletme: panel yöneticisi için global kimlik + işletme başına üyelik — CLAUDE.md §4.5'in üçüncü bilinçli istisnası

- **Durum:** **Kabul edildi — 2026-10-21.** Kullanıcı önce *"Önerilerin hepsini kabul"* dedi (K-1 …
  K-14'ün hepsi (a)). Aynı gün iki bağımsız denetim (güvenlik + üçüncü göz, 33 bulgu —
  orkestratörün `m14-plan-review-1.md` dosyası) **düzeltme gerekli** dedi; kullanıcı ardından
  **K-13'ü (b)'ye çevirdi** (*"üçüncü açık istisna"* — gerekçe §13). Bu sürüm 33 bulgunun hepsini
  işler; bir düzeltmenin kabul edilmiş bir K-kararının anlamını değiştirdiği yer §13'te kararın
  yanında **açıkça** yazılıdır. İkinci denetim turu (`13a26bd` üzerinde, R1–R23 — orkestratörün
  `m14-plan-review-2.md` dosyası) da işlendi; orkestratörün iki netleştirmesi (K-3'ün saati; K-7/K-9'da
  bağlantı tıklamasının posta kutusu kanıtı sayılması) kullanıcının kabul ettiği metne geri döner,
  yeni bir kullanıcı sorusu doğurmaz (§13). (2026-10-21 itibarıyla) **Uygulama başlamadı** — kullanıcı *"başla"* diyene kadar kod,
  migration, CLAUDE.md değişikliği yok; CLAUDE.md metnini M14-01 bu ADR'nin §6.1'inden birebir
  uygular. Plan: [docs/plan/m14-tek-kimlik.md](../plan/m14-tek-kimlik.md).
  **2026-10-22:** M14-01 §6.1 metnini CLAUDE.md'ye uyguladı; **K-15 cevaplandı — (a)**, e-posta
  değiştirme M14'ün dışında, kendi kartı ve kararıyla gelir (§13; open-questions Q34, backlog T173).
  **2026-10-22:** M14-01'in güvenlik denetiminden sonra §6.1'in (3) metni kullanıcı onayıyla
  daraltıldı (bakım yazarları `identity_reconcile()`, backfill ve M14-19'un migration'ı adıyla; `tappa_app`'ın yazamadığı ve
  okuyamadığı bağ sütunları; operatör yasağına `resolve_identity_*`; onay cümlesine `tappa_app` /
  `tappa_resolver` yetkileri) — CLAUDE.md §4.5 (3) ile bayt bayt aynı.
  **2026-10-22:** kapalı listeye iki fonksiyon eklendi — `identity_open_reset` ve
  `identity_withdraw_reset` (M14-14'ün kimlik başına talebinin yazarı listede yoktu; denetim boşluğu,
  kullanıcı onayı; M14-05 yazar — §5.2, §5.9). §6.1'in kapanış cümlesi kimliğe ve `admin_users`'ın bağ
  sütunlarına göre yeniden yazıldı; M14-04'ün iki denetiminin kabul ettiği kart dışı kararlar §5.2 ve
  §5.4'e not edildi.
  **2026-10-23:** kullanıcı kararı (*"Kapat, M14-04'te"*, M14-04'ün güvenlik denetiminden sonra): 
  `identity_open_session` dördüncü bir argüman alır, `p_verified_digest` — Go'nun parolayı karşılaştırdığı
  digest. Fonksiyon kişinin kilidi altında `password_hash = p_verified_digest` değilse 0 satır döner
  (oturum yok, audit yok): eski parolayla başlamış bir giriş, araya giren bir sıfırlama commit edildikten
  sonra oturum alamaz. `tappa_identitydefiner`'ın matrisine `admin_identities` SELECT (`password_hash`)
  eklenir (okunur, döndürülmez) — §5.2'nin imza satırı ve matrisi buna göre.
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
sütun yok, `tappa_app`'ta silme yok — tek istisna aşağıdaki iki adlandırılmış süpürmedir):

| Tablo | Sütunlar (özet) | Not |
|---|---|---|
| `admin_identities` | `id`, `email citext NOT NULL UNIQUE`, `password_hash` (00018'in bcrypt CHECK'i), `email_verified_at NULL`, `confirm_opened_at NULL`, `confirm_open_until NULL`, `confirm_scope NULL CHECK (confirm_scope IN ('first_verify','reset'))`, `last_used_tenant NULL`, `created_at`, `last_login_at` | **`status` sütunu yok** — M14'te kimliği kilitleyen bir yazar yoktur (bulgu 24). Onay penceresi (R1, S3, S5): açılış anı, bitişi ve kapsamı; çözme yalnız `email_verified_at IS NOT NULL AND now() < confirm_open_until` iken (SQL'de denetlenir). Pencere olay anında `now() + 7 gün` açılır; bayrak kapalıyken açılanları M14-19 runbook'u bayrak gününe taşır (§5.8) |
| `admin_identity_resets` | `identity_id`, `token_hash` (00019 biçim CHECK'i), `created_at`, `expires_at` (00019 TTL tavanı), `used_at`, `cancelled_at` | kimlik başına sıfırlama |
| `admin_identity_verifications` | `identity_id`, `token_hash`, `expires_at`, `used_at` | var olan bir kimliğin e-postasını doğrulama (M14-18); **e-posta değiştirme M14'ün dışında** (K-15) |
| `admin_signup_claims` | `token_hash`, `email`, taslak (işletme adı, VAT, lokasyonlar, ad, parola digest'i), `expires_at`, `used_at` | doğrulama tıklamasından önce **hiçbir şey doğmaz** (K-9, bulgu 11). **"Silme yok" kuralının adlandırılmış istisnası** (R17): süresi dolmuş ya da kullanılmış satırları yalnız `tappa_identitydefiner`'ın tek bir süpürme fonksiyonu siler (`identity_sweep_signup_claims`, adlı iş; `tappa_app`'ta DELETE yok) — satır bir taslaktır, kayıt değil |
| `admin_identity_events` | `identity_id NOT NULL`, `event` (kapalı küme), `created_at`, `detail` (kapalı anahtarlar; **e-posta yok, IP yok**) | kimlik düzeyi olaylar: kimliğe karşı başarısız giriş, sıfırlama talebi, doğrulama, birleştirme reddi, çözme. **Bilinmeyen e-posta için satır yazılmaz** — anonim bir append-only tablo sınırsız büyürdü (`adminratelimit.go:740-750`'nin M5-02 dersi); her yazma kimlik kovasıyla sınırlı (§5.6). Saklama: **90 gün**, `00058` deseninde tek silme yolu (`tappa_identitydefiner`'ın süpürme fonksiyonu); okuyan uygulama yolu yok (bulgu 17, R17) |

**Tenant-kapsamlı tablo** (RLS beşlisi + GRANT, CLAUDE.md §6):

| Tablo | Not |
|---|---|
| `admin_invites` | `tenant_id`, `email`, `role`, `token_hash`, `expires_at`, `used_at`, `cancelled_at`, `invited_by` (bileşik FK → `admin_users(id, tenant_id)`). Owner kendi işletmesinin davetlerini RLS içinde listeler ve iptal eder (M7-07 kriteri). Kabul token-hash'e anahtarlı definer ile (§5.2) |

**`admin_users`** (sıcak tablo — her istekte `TouchAdminSession`):
- `identity_id uuid NULL REFERENCES admin_identities(id) ON DELETE RESTRICT` — **kendi
  `lock_timeout`'u olan** ayrı bir ifadeyle; `UNIQUE (tenant_id, identity_id)` indeksi **ayrı bir
  migration dosyasında** (`CONCURRENTLY`, goose no-transaction) — bulgu 19.
- `linked_via text NULL CHECK (linked_via IN ('backfill','reconcile','password','account','invite','signup','claim'))`
  — bağın nasıl kurulduğu; yalnız definer yazar (bulgu 15). Backfill Down'ı yalnız
  `linked_via='backfill'` satırlarını çözer (bulgu 22). **`tappa_app` bu sütunu okuyamaz** (R16):
  `password` / `account` değerleri bir tenant'ın sahibine "bu kişinin başka bir işletmesi var" derdi.
  `signup` = `identity_create_for_signup`'ın kanıtsız bağı; **`claim`** = M14-17'nin parola + posta kutusu
  kanıtlı bağı (S7) — `claim` satırları `first_verify` listesine girmez, `reset` listesine girer (T1).
- `linked_at timestamptz NULL` — bağın kurulduğu an, yalnız definer yazar, `tappa_app` okuyamaz (S3):
  `reset` kapsamlı onay penceresi pencere açılmadan **önce** kurulmuş bütün bağları listeler.
  `CHECK (identity_id IS NULL OR linked_at IS NOT NULL)` — backfill ya da uzlaştırma `linked_at`'i
  sessizce atlayamaz (T12).
- `unlinked_at timestamptz NULL` — **ret damgası** (R1, S4, S8): kişi onay listesinden bir üyeliği
  çözdüğünde definer yazar. Pencere açıkken `identity_relink_refused` ile **geri alınabilir**; pencere
  kapandıktan sonra o satır için geri alma yolu yoktur; üyelik ancak owner'ın **yeni bir davetiyle**
  döner ve davet kabulü **aynı satırı** yeniden bağlar (00006'nın kısmi `UNIQUE (tenant_id, email)`'i,
  `00006:96-97`, reddedilmiş satır e-postasını koruduğu için yeni satır doğamaz — ONAY notu 3, §5.11). Çözme anında satırın süresi dolmamış `password_resets` linkleri
  `cancelled_at` ile iptal edilir (T5) — yoksa eski bir linkin tüketimi tetikleyicinin ikinci koluna
  çarpıp 500 verirdi. Damgalı satır hiçbir yoldan (otomatik bağlama, `identity_link_legacy`,
  uzlaştırma) bağlanmaz, kimlik yolunun eski kolunda **aday sayılmaz**, satır-başı sıfırlama talebi ona
  link göndermez ve tetikleyicinin ikinci kolu `password_hash`'inin definer dışında değişmesini reddeder.
  Eski giriş yolu ve satır-başı sıfırlama talebi `resolve_admin_by_email`'den geçer; bu çözümleyici
  M14-05'te `unlinked_at IS NULL` süzgeciyle ve dönüşüne **`identity_id` eklenerek** yeniden tanımlanır
  (T2, orkestratör kararı (a); dönüş tipi değiştiği için `DROP` + `CREATE` — T5) — bayrak geri alınsa
  bile reddedilmiş satır aday olmaz (S8); tetikleyici kolu ve nöbetçi digest ikinci ve üçüncü
  katmandır. `tappa_app` yazamaz ve okuyamaz.
- 🔴 **`tappa_app` `identity_id` ve `linked_via`'ya INSERT de UPDATE de edemez** (00017'nin sütun
  düzeyi GRANT kalıbı). Sebep `manager.go:285-292`'deki saldırının ikizidir: RLS bir tenant'ın kendi
  satırına istediği `identity_id`'yi yazmasına izin verirdi → saldırgan kurbanın kimliğini kendi
  işletmesine "üye" yapar, kurbanın değiştiricisine kendi işletmesini düşürürdü.
- `tappa_app`'ın `UPDATE (email)` yetkisi **expand'de** kapanır (hiçbir sorgu yazmıyor — §2) →
  K-14'ün aynası ilk günden yapısal (bulgu 21).
- **SELECT tablo düzeyinden sütun düzeyine iner** (S9): `00006:115` `tappa_app`'a `admin_users` üzerinde
  **tablo düzeyi** SELECT verir; tablo yetkisi dururken sütun REVOKE'u etkisizdir ve tablo yetkisi gelecek
  sütunları da kapsar. M14-03 `REVOKE SELECT ON admin_users FROM tappa_app` + `GRANT SELECT (id,
  tenant_id, full_name, email, password_hash, role, status, created_at, last_login_at, identity_id)` yapar;
  `linked_via`, `linked_at`, `unlinked_at` gizli kalır. Bugün `admin_users` üzerinde `*` seçimi yok (grep),
  dönüşüm güvenli.
- `tappa_app` `SELECT (identity_id)`'yi **korur**: `TouchAdminSession` geçiş oran kovası için onu
  döndürür (§5.4). Bedel: tenant-ötesi bir korelasyon anahtarı uygulama belleğine girer — **§4.5'in
  adlandırılmış kalıntısı** (§6.1). Sınır: bir sorgu-metni taraması `identity_id`'yi
  `TouchAdminSession` dışındaki **hiçbir** sqlc sorgusunun seçmediğini ve `admin_users` üzerinde `*`
  / `a.*` seçimi olmadığını, bir görünüm-tipi taraması onun hiçbir şablon/CSV/API tipine girmediğini
  sabitler (bulgu 21'in "düşün" maddesi bu yönde karara bağlandı; R16).
- **Bağlı satırı koruyan tetikleyici** (bulgu 6a, R6): `BEFORE UPDATE ON admin_users`, `OLD.identity_id
  IS NOT NULL` ve (`password_hash` ya da `email` değişiyor) ve `current_user <>
  'tappa_identitydefiner'` → `RAISE` (fail-closed). Tetikleyici fonksiyonu **SECURITY INVOKER'dır,
  asla DEFINER değildir** — DEFINER olsaydı `current_user` gövdede fonksiyonun sahibi olurdu ve denetim
  hiç tetiklenmezdi; ADR 0028 §1'in kuralıyla `EXECUTE` yalnız sahibinde (`REVOKE EXECUTE … FROM
  PUBLIC`, `00061` başlığı); `CREATE TRIGGER` kendi `lock_timeout`'uyla. Dual-write **tek yönlüdür:
  kimlik → satırlar**. Satır → kimlik eşitleyen bir tetikleyici **yazılmaz**: tenant A'nın bir yazması
  tenant B'nin satırına taşınırdı. **İkinci kol** (S8): `OLD.unlinked_at IS NOT NULL` ve
  `password_hash` değişiyor ve `current_user <> 'tappa_identitydefiner'` → `RAISE` (bağlanmamış ama
  reddedilmiş satır, posta kutusu sahibinin satır-başı sıfırlamasıyla kullanılabilir bir parola alamaz).
  Başka bir aşma yolu bulunmadı (operatörün tek `admin_users` yetkisi
  `SELECT (tenant_id, email, status)`, `00029:169`). **Adlandırılmış bedel:** expand rollout
  penceresinde eski pod'un bağlı bir satıra parola değişimi / sıfırlama tüketimi 500 alır (birkaç
  dakika; kabul). Contract B'nin tetikleyiciyle ilişkisi §8'de.
- `password_hash`: bağlı satırda kimliğin digest'inin kopyası (dual-write, contract'a dek NOT NULL);
  davet ve kayıtla doğan bağlı satırlar da kopyayı taşır — **test edilen değişmez:** bağlı her satırda
  `password_hash = kimliğin digest'i` (bulgu 6).
- `full_name`, `role`, `status` üyelikte kalır (K-8, K-14).

**`admin_sessions`:** tek ekleme `authenticated_at timestamptz NULL` — oturumun parola kanıtının
anı. Girişte `now()` (definer yazar), geçişte **eski oturumun veritabanı satırından** kopyalanır,
istemciden asla (bulgu 14, R15). Sütun önce `NULL` olarak eklenir, `DEFAULT` sonra ayrı bir ifadeyle
konur (`ADD COLUMN … DEFAULT now()` eski satırlara ALTER anını yazardı); `ALTER` kendi
`lock_timeout`'uyla; `tappa_app` UPDATE edemez. **`NULL` = eski / süresi geçmiş** sayılır: tazelik
isteyen her denetim (R3) ve ileride gelecek mutlak ömür bunu fail-closed okur. Oturum hâlâ tek
üyeliğe, tek tenant'a damgalıdır.

**M14-08a notu — adres kuralı (2026-10-26; kullanıcı kararı):** bir kimliği **doğurabilen ya da ona
anahtar olabilen** her saklı adres `mail.ValidRecipient`'ten geçer — `internal/mail`'in `Send`'inin
uyguladığı kural (≤ 254 oktet, `=?` yok, net/mail'in kendine geri verdiği yazdırılabilir-ASCII
addr-spec). Geçmeyen bir satır hiçbir kimliğe bağlanmaz ve kimlik doğurmaz. **Neden** (M14-08'in
güvenlik denetimi; canlıyla aynı imaj ve yerel ayar, PostgreSQL 17.10 musl, 0x80–0x10FFFF taranarak):
citext eşitliği tam iki ASCII-dışı kod noktasını ASCII'ye katlar — U+0130 (→ `i`) ve U+212A KELVIN SIGN
(→ `k`); adresin danışma kilidinin anahtarı da eşittir. Kayıt her UTF-8 adresi kabul ettiği için
homoglifi **önce** yazan, kimliği kendi yazımıyla doğururdu; posta yalnız yazdırılabilir-ASCII alıcılara
gider, yani gerçek posta kutusunun sahibi o adrese hiçbir zaman sıfırlama alamazdı (devralma yok,
numaralandırma kanalı yok; etkisi kalıcı erişim kaybı). **`admin_identities.email` üzerinde CHECK
yok — ölçülerek:** ihlalin `DETAIL`'i (`Failing row contains …`) BYPASSRLS definer'ın SELECT edebildiği
her sütunu, adresi **ve** digest'i, hataya ve sunucu log'una basar (00073 ve 00074'ün digest için
belgelediği tuzak; CLAUDE.md §7). Kural yüklemlerdedir: her kimlik INSERT'i kuralı INSERT'ten **önce**
karar verir (§5.2'nin M14-08a notu, §5.8'in M14-08a notu). **Düşünülen ama kurulmayan seçenek**
(M14-08a güvenlik denetimi, 2026-10-27): sabit mesajlı bir `BEFORE INSERT OR UPDATE` tetikleyicisi —
adresi hiçbir mesaja koymadan reddeden, sızıntısız bir veritabanı arkalığı. §5.2'nin kapalı
listesine bir fonksiyon ekler; bu yüzden kullanıcı kararıdır ve bu kartta yapılmadı.

### 5.2 Roller ve fonksiyonlar — kapalı liste

**`tappa_identitydefiner`**: NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesi yok, varsayılan yetkisi yok,
yalnız sütun düzeyi GRANT; `01-roles.sql`'de yaratılır (migration'da değil — R5b, ADR 0021
`:759-763`) ve canlı kümeye runbook'la, **migration'dan önce** uygulanır (`tappa_holdingdefiner`
emsali, `01-roles.sql:133-154`). Katalog testlerindeki rol listeleri (`operatorschema_test.go:513-517`,
`:556-570`; `m11schema_test.go:94`) ve `privcatalog_test.go`'nun `privCatalogAllowed` izin listesi
(CLAUDE.md §6, T137) aynı commit'te genişler (bulgu 19).

**ADR 0002 md.7 çözümleyicileri** (salt okuma, sahibi `tappa_resolver`, ≤ 1 satır, UNIQUE'ten, hiçbiri
yazmaz):

| Fonksiyon | Anahtar | Dönüş |
|---|---|---|
| `resolve_admin_by_email(p_email citext)` — **yeniden tanım** (M14-05, T2) | e-posta | `(id, tenant_id, password_hash, status, identity_id)`, `unlinked_at IS NULL` süzgeci, incumbent-first sıra aynen. **md.7 (iii) genişlemesi, gerekçesiyle:** kimlik doğrulanmadan önce dönen yüzeye `identity_id` eklenir — kimlik yolunun eski kolu "bağlanmamış satırlar"ı ancak bununla ayırır (Go, kimliğin **kendi** satırlarını atlar); aksi hâlde kimlik digest'inin kopyasını taşıyan bağlı satırlar 8'lik pencereyi doldurur, doğru parola boşuna "eski satırı doğrular" ve 8+ üyelikli bir kişinin bağlanmamış satırları hiç otomatik bağlanmaz. Değer bir korelasyon anahtarıdır, sır değildir; giriş yanıtına, log'a ve audit'e girmez. Eski binary uyumlu: çağıran sütunları adıyla seçer (`internal/db/resolve.go:301-302`). Bayrak `off` iken eski giriş bugünkü gibi bağlı satırları da görür |
| `resolve_identity_by_email(p_email citext)` | e-posta | `(id, password_hash, email_verified_at, has_active_membership, email)` — giriş ve (M14-14) kimlik başına sıfırlama talebi. Global UNIQUE sayesinde md.7 (ii) "≤ 1" giriş için geri gelir (bugün e-posta istisnadır, ADR 0002 `:316-322`). `has_active_membership` beşinci zamanlama durumunu kapatır (§5.6, R14). `email` (00075, 2026-10-26 notu): kimliğin **kayıtlı** yazımı — sıfırlama linkinin alıcısı, `id` ile aynı satırdan |
| `resolve_identity_reset_by_token_hash(p_hash)` | sıfırlama token hash'i | `(id, identity_id, expires_at, used_at, cancelled_at)` |
| `resolve_admin_invite_by_token_hash(p_hash)` | davet token hash'i | `(id, tenant_id, role, expires_at, used_at, cancelled_at)` |
| `resolve_signup_claim_by_token_hash(p_hash)` | kayıt token hash'i | `(id, email, password_hash, expires_at, used_at, draft)` — digest ve taslak yalnız talep sırrıyla döner (S12; `draft`: 2026-10-26 kullanıcı kararı, aşağıdaki M14-17 notu) |
| `resolve_identity_verification_by_token_hash(p_hash)` | doğrulama token hash'i | `(id, identity_id, expires_at, used_at)` |

**`identity_*` fonksiyonları** (sahibi `tappa_identitydefiner`; yazarlar). **Kural: kimliği yazan her
fonksiyon kimliği yalnız bir sırdan bulur** — canlı oturum (`admin_sessions.token_hash` →
`admin_users.identity_id`, **NOT NULL** olmalı) ya da sıfırlama / davet / kayıt / doğrulama
token'ı. **E-postadan kimliğe giden tek yol `resolve_identity_by_email`'dir ve hiçbir şey yazmaz**
(bulgu 2). Sırra anahtarlı olmayan **üç adlandırılmış istisna** vardır (`identity_open_session`,
`identity_create_for_signup`, `identity_record_failed_login`); her biri neden zararsız olduğunu ve onu
yapısal kılan yüklemleri taşır. Bunlara ek olarak **kimlik yazmayan** ama sırra da anahtarlı olmayan yazarlar
adıyla sayılır (S11): `identity_open_signup_claim` (yalnız taslak satırı, oranla sınırlı),
`identity_open_reset` (yalnız bir sıfırlama linki + bir olay satırı, kimlik başına tek canlı link) ve
iki sabit periyotlu süpürme. ⚠️ işaretlilerin hepsi bunlardır. Gövdeler BYPASSRLS ile koştuğu için
tenant tablosuna dokunan **her** ifade açık bir `tenant_id` yüklemi taşır (kuşak + kemer, ADR 0021
§3.2'nin `op_*` kuralı).

| Fonksiyon | Anahtar | Ne yapar / ne zaman reddeder |
|---|---|---|
| `identity_open_session(p_identity_id, p_tenant, p_token_hash, p_verified_digest)` | ⚠️ **sır değil — istisna 1** | Parolası uygulamada doğrulanmış kimlik için istenen / son kullanılan / en eski aktif üyelikte oturum açar, `authenticated_at = now()`, o tenant'a giriş audit'i. Kişinin kilidi altında kimliğin digest'i hâlâ `p_verified_digest` değilse 0 satır (2026-10-23 kullanıcı kararı: araya giren sıfırlama eski parolanın girişini düşürür). Tek çağrı yolu Go'daki doğrulanmış-kimlik tipidir (00011 YÜKÜMLÜLÜK 5'in karşılığı; bugünkü `Issue(Verified)` ile aynı güven) |
| `identity_create_for_signup(p_tenant_id, p_admin_user_id)` | ⚠️ **sır değil — istisna 2, yalnız expand → M14-17 arası** | Kayıt transaction'ı içinde. **Neden zararsız** (R13): etkisi uzlaştırmanınkiyle aynıdır — tek satırlı bir e-posta grubundan kimlik. **Yapısal yüklemler:** tenant'ın **tek** `admin_users` satırı bu satırdır (asıl güvence — T7), satır bu transaction'ın başında doğmuş (`admin_users.created_at = now()`; `now()` transaction başlangıcıdır, alt transaction'dan etkilenmez, `created_at` uygulamaya yazılamaz — `00017:379-416`) ve **ek olarak** `xmin = pg_current_xact_id()::xid` (xmin "bu transaction'da **yazılmış**" demektir, "eklenmiş" değil — aynı transaction'daki bir UPDATE de onu koyar), `p_tenant_id = current_setting('app.tenant_id')`; e-posta hiçbir kimlikte ve başka hiçbir `admin_users` satırında yok → kimlik + bağ (`linked_via='signup'`); varsa hiçbir şey, satır bağlanmamış doğar. **Eşzamanlı iki kayıt** (READ COMMITTED'da ikisi de "başka satır yok" görür): repodaki ad alanlı kilit deseniyle (`passwordresets.sql:353`, `invites.sql:404`) `pg_advisory_xact_lock(hashtextextended('admin-identity-email:' || lower(email), 0))` — tap debounce'unun kilit uzayıyla çakışmaz (S13) — + kimlik INSERT'inde `ON CONFLICT (email) DO NOTHING` → ikincisi bağlanmamış satırla biter, **500 yok**. **SAVEPOINT tuzağı** (S13): `xmin` yüklemi bir alt transaction'da (pgx iç içe `Begin`) yazılan satırda tutmaz; sonuç güvenli (satır bağlanmamış doğar) ama sessizdir — kart testi **gerçek `Provision` yolundan** koşar ve kimlik doğmazsa kırmızıdır; `pg_current_xact_id()::xid` hedef PostgreSQL sürümünde (17.10) doğrulanır. M14-17 onu çağırmayı bırakır; **DROP bir sonraki deploy'dadır** (contract B, M14-20b — R8: rollout penceresindeki eski pod düşmüş bir fonksiyonu çağırmasın) |
| `identity_record_failed_login(p_identity_id, p_event)` | ⚠️ **sır değil — istisna 3** (R9) | Parolası **doğrulanmamış** bir kimliğe karşı başarısız giriş: yalnız `admin_identity_events`'e bir satır yazar, başka hiçbir şeye dokunmaz. Sınır uygulamadadır: kimlik başına `adminAccountLimit` kovası (`adminratelimit.go:740-781`'in deseni) — limit aşılınca yalnız **bir** `FirstOverLimit` satırı, sonra hiç |
| `identity_memberships(p_session_hash)` | canlı oturum | Kişinin aktif üyelikleri, ≤ 50, `(tenant_id, tenant_name, role, suspended, is_current)`; oturumun satırı bağlı değilse yalnız kendisi |
| `identity_switch_session(p_session_hash, p_target_tenant, p_new_hash)` | canlı oturum | §5.4 |
| `identity_revoke_sessions(p_session_hash, p_keep_current)` | canlı oturum | Kişinin **bütün** işletmelerdeki oturumlarını iptal; her tenant'a kendi sıradan çıkış satırı |
| `identity_password_for_session(p_session_hash)` | canlı oturum | Hesap sayfasında mevcut parolayı doğrulamak için digest; satır bağlı değilse 0 satır (uygulama bugünkü tek-satır yolunu kullanır) |
| `identity_set_password(p_session_hash, p_new_hash)` | canlı oturum | Kimliğe **ve** bağlı bütün satırlara yazar, öbür oturumları bütün işletmelerde iptal eder; satır bağlı değilse **0 satır** ve uygulama parolayı **yalnız o satıra** yazar (bugünkü gibi) |
| `identity_consume_row_reset(p_reset_hash, p_new_hash)` | **eski** `password_resets` token'ı | M14-08'den itibaren: link bağlı bir satırınsa kimliği + bağlı satırları yazar, bütün oturumları iptal eder, **`email_verified_at`'i doldurur** (linke tıklayan posta kutusunun sahibidir — S5; aksi hâlde çözme hiç mümkün olmazdı) ve **`reset` kapsamlı** onay penceresini açar (ADR 0015'in anlam değişikliği **burada başlar**, bulgu 6); bağlı değilse 0 satır (eski yol). `unlinked_at` damgalı satıra hiçbir zaman link gönderilmez (S8) |
| `identity_consume_reset(p_reset_hash, p_new_hash)` → `TABLE (identity_id uuid, revoked_sessions integer, email text)` | kimlik sıfırlama token'ı | M14-14: kimlik + bağlı satırlar, `email_verified_at`, bütün oturumlar iptal, oturum vermez; bağlanmamış eski satırları **bağlamaz**; **her** tüketimde **`reset` kapsamlı** onay penceresi (kimliğin denetimi el değiştirdiği an — R2, S3). `email` (00075): kimliğin kayıtlı adresi — kişi başına tek "parolanız değişti" bildiriminin (EM-9, §5.14) alıcısı |
| `identity_open_reset(p_identity_id uuid, p_token_hash text, p_expires_at timestamptz)` → `TABLE (reset_id uuid, expires_at timestamptz, issued boolean)` | ⚠️ **sır değil — yazar ama kimlik yazmaz** (2026-10-22, kullanıcı onayı) | M14-14'ün kimlik başına talebi: kimliği çağıran e-postadan `resolve_identity_by_email` ile bulur; `p_token_hash` çağıranın o anda ürettiği yeni token'ın hash'idir. Kimlik başına ad alanlı danışma kilidi altında (`pg_advisory_xact_lock`, bugünkü `LockAdminForResetIssue` deseni) canlı link okunur (`used_at` ve `cancelled_at` NULL, süresi dolmamış): **canlı link varsa** hiçbir şey basmaz, hiçbir şeyi iptal etmez, hiçbir şey yazmaz ve o linkin `reset_id` / `expires_at`'ini `issued = false` ile döndürür (EM-7C, §5.9); **yoksa** tek bir `admin_identity_resets` satırı + tek bir `reset_requested` `admin_identity_events` satırı (`detail` boş) yazar, `issued = true`. Kimliğe ve `audit_log`'a yazmaz; e-posta, IP, tenant ve üyelik sayısı hiçbir satıra girmez. Sınır: uygulamanın istek bütçesi + kimlik başına tek canlı link |
| `identity_withdraw_reset(p_token_hash text)` → `boolean` | kimlik sıfırlama token'ı (çağıranın az önce bastığı linkin hash'i) | Bugünkü `Withdraw`'ın karşılığı (`resetrequest.go:290-312`): teslim edilemeyen bir linki geri çeker ki sonraki talep yenisini basabilsin. Yalnız kullanılmamış ve iptal edilmemiş linkte `cancelled_at = now()`; geri çekildiyse `true`, link zaten gitmişse `false` (hata değil). Başka hiçbir şey yazmaz: bugünkü tenant satırlarının (`admin.recovery.undelivered` / `kept`) kimlik düzeyinde karşılığı yoktur — kimlik tenant'sızdır ve iz açılıştaki `reset_requested` satırıdır |
| `identity_link_legacy(p_identity_session_hash, p_legacy_session_hash)` | **iki** canlı oturum | Biri bağlı (kimlikli), öbürü bağlanmamış ve `unlinked_at IS NULL` bir satırın oturumu; **ikisinin de `authenticated_at > now() - interval '5 minutes'`** (R3; `NULL` → 0 satır). Akışlar iki tarafta da o istekte **taze** oturum açar — hesap sayfasında bu, mevcut oturumun parolasının da yeniden sorulması demektir (S6). Savunma katmanı: iki satırın e-postası eşit (`OPERATOR(public.=)`). Bağlanan satırın digest'i kimliğinkine eşitlenir (contract B'ye dek), `linked_at = now()`, eski oturum iptal, o tenant'a "linked" (karşı tenant'ı anmadan) |
| `identity_unlink_backfilled(p_session_hash, p_tenant)` | **taze** canlı oturum (S4) | Oturumun `authenticated_at`'i son 5 dakika içinde (uygulama çözmeden önce kimlik parolasını **yeniden sorar** ve taze oturum açar), `email_verified_at IS NOT NULL`, `now() < confirm_open_until`; listelenebilir satır kümesi pencerenin kapsamına bağlıdır: `first_verify` → `linked_via IN ('backfill','reconcile','signup')`; **`reset` → `linked_at < confirm_opened_at` olan bütün üyelikler, `claim` ve `invite` dahil her `linked_via`** (S3, T1: önceden ekilmiş bir kimlik, kurbanın posta kutusuna giden bir kayıt ya da davet linkini sosyal mühendislikle iletilmiş olarak kendi oturumuyla tıklayıp `claim` / `invite` bağı da kurabilir — kimliğin denetimi el değiştirdiğinde hepsi görünür); `first_verify` kapsamı `claim` / `invite` istisnasını korur. `created_at < contract anı` olan satır çözülür; **contract B'den sonra doğmuş** bir satır için "çöz" **bağı korur, `unlinked_at`'i damgalar ve `status='disabled'` yapar** (ONAY notu 2): `status` tek başına yetmez — `tappa_app`'ın `UPDATE (status)` yetkisi var (`00017:380-381`) ve M14-16'nın panel yolu, saldırganın tenant'ındaki başka bir owner'ın onu yeniden açmasına izin verirdi; `unlinked_at`'i uygulama yazamaz. `identity_memberships`, `identity_switch_session`, `identity_open_session` ve yeniden tanımlanan `resolve_admin_by_email` `unlinked_at IS NULL` süzer; `TouchAdminSession` **süzmez** — `tappa_app` `unlinked_at`'i okuyamaz (R16). Güvence şuna dayanır: çözme o tenant'ın oturumlarını **aynı transaction'da** iptal eder, oturum veren her yol `unlinked_at`'i süzer ve contract sonrasında satır `status='disabled'` olur. Oturum veren yollar çözümleme anında süzer; eski kolun `CreateAdminSession`'ı (`db/queries/admins.sql:117-123`) satırı yalnız `status = 'active'` ile yeniden okur ve `unlinked_at`'e bakamaz (R16). Yükümlülük (M14-07, eski kol): `CreateAdminSession`'ın `INSERT … SELECT`'i `a.password_hash = <doğrulanan digest>` yüklemini **ve** `FOR NO KEY UPDATE OF a` kilidini taşır (2026-10-23, M14-07 denetimi: önce `FOR SHARE OF a` yazılmıştı; aynı transaction'daki `MarkAdminLoggedIn` UPDATE'i paylaşımlı kilidi yükseltmeye çalıştığından aynı yöneticinin iki eşzamanlı `Issue`'su 40P01 ile kilitleniyordu — 86/120 ölçüldü; `FOR NO KEY UPDATE` ile 0/120, çözmenin `FOR UPDATE`'iyle yine çakışır); kilit çözmeyle çakışır, READ COMMITTED yüklemi yeni sürümde yeniden değerlendirir → 0 satır (çözme nöbetçi digest'i aynı transaction'da yazar; kilitsiz biçim, çözme transaction'ı açıkken başlayan bir yazımda canlı oturum bırakıyordu — PostgreSQL 17.10'da ölçüldü). Satır kilidi bir UPDATE yetkisi ister: `tappa_app`'ın `UPDATE (status)`'ı (`00017:380-381`) yeter; M14-07 FORCE RLS altındaki UPDATE politikasını ölçer. Contract B'den sonra eski kol yoktur. `identity_id` korunduğu için VALID CHECK'e dokunulmaz. Bu yol M14-16'nın son-owner değişmezinin **bilinçli istisnasıdır**: değişmez bir veritabanı tetikleyicisiyse definer'ı burada durdurmaz; arayüz contract öncesindeki gibi **uyarır ve izin verir** (ONAY notu 4) — T1. Satırı bağlanmamışa çevirir, `unlinked_at = now()`, `password_hash`'e **nöbetçi digest** (32 rastgele baytın bcrypt'i; baytlar atılır, repoda sabit yok — S20; contract B'den sonra `NULL` — T3), satırın süresi dolmamış `password_resets` linklerini `cancelled_at` ile iptal eder (T5), o tenant'taki oturumları iptal eder. Satır tenant'ın **son aktif owner'ı** ise arayüz açık bir uyarı gösterir (S4) |
| `identity_relink_refused(p_session_hash, p_tenant)` | **taze** canlı oturum + kimlik parolasının yeniden girilmesi | S4/T4: çözme pencere açıkken **geri alınabilir**. Yüklem: `tenant_id = p_tenant` **ve** `email OPERATOR(public.=) identity.email` (e-posta global tekil; K-15 kapsam dışı) **ve** `unlinked_at >= confirm_opened_at` (yalnız **bu** pencerede reddedilmiş satır) **ve** `now() < confirm_open_until`; o tenant'ta kimliğin başka bir üyeliği varsa (arada davetle doğmuş — `UNIQUE (tenant_id, identity_id)`) 0 satır. Satırı yeniden bağlar (`linked_via='account'`, **özgün `linked_at` korunur** → satır pencerenin geri kalanında listede kalır; ele geçirilmiş 5 dakikalık bir oturumun ya da yanlış tıklamanın etkisi aynı pencerede geri alınabilir), `unlinked_at = NULL`, digest kopyası contract B'ye dek. Pencere kapandıktan sonra satır için geri alma yolu **yoktur**; üyelik ancak owner'ın **yeni bir daveti** ile döner ve kabul **aynı satırı** yeniden bağlar (§5.11, ONAY notu 3). Arada "davetle doğmuş başka üyelik" dalı bu yüzden oluşamaz (aynı tenant'ta aynı e-postalı ikinci satır `00006:96-97` ile imkânsızdır); 0 satır kuralı savunma olarak kalır |
| `identity_request_verification(p_session_hash, p_token_hash)` | canlı oturum | Doğrulama token'ını **isteyen kimliğe** bağlı yazar |
| `identity_verify_email(p_verification_hash, p_session_hash)` | doğrulama token'ı + tıklayan tarayıcıdaki **aynı kimliğin** canlı oturumu | Tıklayan tarayıcıda token'ı isteyen **kimliğin** canlı bir oturumu yoksa 0 satır (R2: başka bir tarayıcıdan tıklama kimseyi doğrulamaz). Bağ oturuma değil kimliğe: istek ile tıklama arasında işletme değiştiren (yeni oturum) kişi kırılmaz (S16). `email_verified_at`; ilk doğrulamada `first_verify` kapsamlı onay penceresi |
| `identity_confirm_window(p_session_hash)` → `TABLE (verified boolean, scope text, open_until timestamptz, tenant_id uuid, tenant_name text, role text, refused boolean, last_active_owner boolean)` | canlı oturum (taze değil) | M14-18, 2026-10-27 kullanıcı kararı: onay penceresinin **okuyucusu** (STABLE). İlk satır başlıktır (`tenant_id` NULL; `verified`, pencere açıksa `scope` ve `open_until`); pencere açıkken (`email_verified_at` dolu **ve** `now() < confirm_open_until`) ardından kapsamdaki üyelikler (`identity_unlink_backfilled`'ın iki kapsam testi aynen) ve **bu pencerede** reddedilmiş satırlar (`refused`, `identity_relink_refused`'ın yüklemi: kimliğin adresi, `identity_id IS NULL`, `unlinked_at >= confirm_opened_at`). `last_active_owner`: satır aktif owner ve işletmesinde başka aktif, reddedilmemiş owner yok (S4 uyarısı; sayı değil). Toplam en çok 50 satır. Bağlanmamış satırın oturumu 0 satır. Kemer: oturum kendi anahtarıyla, üyelik listesi kimlikle sınırlı adlı geniş okuma, reddedilmiş satırlar adlı e-posta-geniş okuma (kimliğin kendi adresi) |
| `identity_keep_all(p_session_hash)` → `TABLE (kept boolean)` | canlı oturum (taze değil — **tek tık**, T11) | M14-18, 2026-10-27 kullanıcı kararı: **"Keep all"** — açık pencerenin üç sütunu (`confirm_opened_at`, `confirm_open_until`, `confirm_scope`) **tek UPDATE'te** NULL (00069'un `admin_identities_confirm_window_whole` CHECK'i başka biçimi reddeder), `email_verified_at` korunur, tek `confirm_kept` olayı (`detail` yalnız kapatılan `scope`). Kimlik kilidi önce, oturum kilit altında yeniden denetlenir; pencere kilitlerden sonra hâlâ açık değilse 0 satır. Üyeliğe ve `audit_log`'a yazmaz. Sonrasında çözme ve geri alma pencere kapalı olduğu için 0 satırdır |
| `identity_accept_invite(p_invite_hash, p_session_hash)` | davet token'ı + canlı oturum | `identity.email = invite.email` (`OPERATOR(public.=)`) değilse 0 satır (iletilmiş link, bulgu 12). Üyelik doğar (`linked_via='invite'`, digest kopyası); **yeni oturum vermez**. Tıklama, aynı e-postalı kimliğin canlı oturumuyla yapıldığı için **posta kutusu kanıtıdır**: `email_verified_at` doluysa dokunulmaz, boşsa doldurulur ve onay penceresi açılır (orkestratör kararı, §13 K-7) |
| `identity_accept_invite_new(p_invite_hash, p_new_hash)` | davet token'ı | Adreste kimlik yoksa: yeni kimlik (`email_verified_at` = tıklama) + üyelik; **oturum vermez** (ADR 0015 §1 deseni). Adreste kimlik varsa 0 satır. Kimlik INSERT'i `identity_create_for_signup` ile aynı e-posta kilidini alır ve `ON CONFLICT (email) DO NOTHING` (eşzamanlı davet kabulü + kayıt onayı → 500 yok, S13) |
| `identity_open_signup_claim(p_token_hash, p_email, p_draft jsonb, p_password_hash, p_expires_at)` | ⚠️ **sır değil — yazar ama kimlik yazmaz** (S11) | Yalnız `admin_signup_claims`'e bir taslak satırı; başka hiçbir tabloya dokunmaz. Anahtar, çağıranın o anda ürettiği yeni token + e-posta; sınır `signupCreateLimit` + alıcı başına üst sınır. `p_draft` kapalı anahtarlı (ad, VAT, işletme adı, lokasyonlar; 2026-10-26 kullanıcı kararıyla + `business_type`, `structure`, `vat_check`); doğrulayıcı uygulamadadır |
| `identity_claim_signup(p_claim_hash, p_tenant_id, p_session_hash NULL, p_verified_digest NULL)` | kayıt token'ı (+ var olan kimlikte canlı oturum; **aktif üyeliği olmayan** var olan kimlikte doğrulanmış digest — M14-17b) | M14-17 (§5.10). **Tenant'ı uygulama yaratır** (bugünkü `Provision`, `WithTenant`); definer talebi tüketir ve kimliği + owner satırını yazar (`linked_via='claim'`, S7). **Yapısal yüklemler (S2, T7) — biri tutmazsa 0 satır:** tenant'ın hiç `admin_users` satırı yok (**asıl güvence**), `tenants.created_at = now()` (transaction başlangıcı; alt transaction'dan etkilenmez; `created_at` uygulamaya yazılamaz — `00016:84-88`'in INSERT/UPDATE sütun listelerinde yok), ek olarak `tenants.xmin = pg_current_xact_id()::xid` (denetçinin PostgreSQL 17.10 ölçümü: üst düzey transaction'da doğru, SAVEPOINT altında yanlış — kayıt üst düzey `WithTenant` kullanır, `signup.go:689`, SAVEPOINT'li `WithTenantIn` değil, `internal/db/tenant.go:115-119`; `GRANT SELECT (xmin)` mümkün), `p_tenant_id = current_setting('app.tenant_id')`, tenant'ın VAT'ı ve adı talebin taslağına eşit. Var olan kimlikte `identity.email = claim.email` (R18). Yeni adreste kimlik talebin saklı digest'ini alır; **parola karşılaştırması uygulamadadır** — çözümleyici talebin digest'ini yalnız talep sırrıyla döndürür, uygulama yeniden girilen parolayı onunla karşılaştırır ve fonksiyonu yalnız eşleşmede çağırır (Go tipiyle bağlı güvence, `identity_open_session` gibi — S12). Kimlik INSERT'i e-posta kilidi + `ON CONFLICT DO NOTHING` (S13). **M14-17b (2026-10-27 kullanıcı kararı, `00080`):** dördüncü argüman `p_verified_digest` — aktif üyeliği olmayan var olan kimliğin parola kanıtı (aşağıdaki M14-17b notu); oturum **ve** digest birlikte → 0 satır |
| `identity_sweep_signup_claims()` / `identity_sweep_events()` | ⚠️ **sır değil — sabit periyotlu silme** (S11) | Süresi dolmuş taleplerin ve 90 günü geçmiş olayların tek silme yolu (R17); parametresiz, yalnız süreye göre |

**M14-04 uygulama notları (2026-10-22; iki denetimin kabul ettiği kart dışı kararlar):**
`identity_open_session` ve `identity_switch_session` `(session_id, tenant_id, admin_user_id)` döndürür.
`identity_open_session`'ın iniş sırası istenen → son kullanılan → en eski aktif üyeliktir; istenen üyelik
aktif değilse sessizce bir sonrakine düşer — Go çağıranı dönen `tenant_id`'yi istenenle karşılaştırmalıdır.
`identity_revoke_sessions` `TABLE (revoked integer)` döndürür ve iptal ettiği her oturum için o oturumun
tenant'ına bir sıradan çıkış satırı yazar — **yalnız aktif ve reddedilmemiş** (`unlinked_at IS NULL`)
üyeliklerde.

**M14-17 notu (2026-10-26 kullanıcı kararı — S1 "Dönüşe taslak ekle"; uygulama `00078`):**
`resolve_signup_claim_by_token_hash` dönüşünün **sonuna** `draft jsonb` eklenir. **Neden:** tenant'ı ve
lokasyonlarını uygulama yaratır (`identity_claim_signup`'ın yüklemleri o tenant'ın bu transaction'da
doğmuş olmasını ister; definer'da `tenants` INSERT yoktur), yani onay POST'unda işletme adı, VAT, tür,
yapı, VIES sonucu, lokasyonlar ve ad elde olmalıdır; cihazlar arası çalışma (§5.10) imzalı çerezi dışarıda
bırakır ve `tappa_app`'ın kimlik tablolarında hiçbir yetkisi yoktur — tek okuma yolu çözümleyicidir.
**md.7 (iii):** taslak yalnız talep sırrına karşı döner ve içeriği talebi açanın kendi yazdığıdır (00075'in
`email` emsaliyle aynı türden). Taslağın kapalı anahtar kümesi aynı kararla `business_type`, `structure` ve
`vat_check` ile genişler: `full_name`, `business_name`, `vat_number`, `business_type`, `structure`,
`vat_check`, `venues` (1–25 metin) — `00078`'in CHECK'i ve `identity_open_signup_claim`'in ön denetimi
aynı kümeyi sabitler. Kart içi kararlar (denetime açık): dönüş tipleri `identity_open_signup_claim` →
`uuid` (red = NULL, hiçbir şey yazılmaz), `identity_claim_signup` → `TABLE(admin_user_id uuid)`,
`identity_sweep_signup_claims` → `integer`; `identity_claim_signup` kaydın `signup.completed` satırını
(uygulamanın detay anahtarlarıyla, iki yolda aynı satır) **kendisi** yazar — "tenant'a yazan fonksiyon o
tenant'ın audit'ine yazar" kuralı; yeni adreste kimlik `INSERT (email, password_hash)` + `UPDATE
email_verified_at` ile doğar (`INSERT (email_verified_at)` verilmez — 00074'ün son koşulu), **onay penceresi
açılmaz** (`claim` bağı `first_verify` listesine girmez, listelenecek bağ yoktur); var olan kimlikte
`email_verified_at` boşsa dolar ve `first_verify` penceresi + `email_verified` olayı yazılır. ASCII adres
kuralı (kullanıcı kararı 2026-10-26, M14-08a): `identity_open_signup_claim` ve `identity_claim_signup`
yazdırılabilir ASCII olmayan bir adresi reddeder. Matris eki: `admin_signup_claims` SELECT (`id`,
`token_hash`, `email`, `draft`, `password_hash`, `expires_at`, `used_at`), INSERT (`token_hash`, `email`,
`draft`, `password_hash`, `expires_at`), UPDATE (`used_at`), DELETE (yalnız `identity_sweep_signup_claims`);
`admin_users` INSERT (matrisin hücresinin tamamı); `tenants` SELECT (`vat_number`) ve `xmin`;
`tappa_resolver`: `admin_signup_claims` SELECT (anahtar + döndürdüğü altı sütun).

**M14-17b notu (2026-10-27 kullanıcı kararı — "parola kanıtıyla sahiplenme", seçenek (A) "Dördüncü
parametre"; uygulama `00080`, backlog T170):** bütün üyelikleri devre dışı olan bir kimlik yeni işletme
kaydedemiyordu: kayıt bilinen adresi var olan kimlik koluna yönlendirir, o kol kimliğin canlı oturumunu
ister, aktif üyelik olmadığı için giriş mümkün değildir; yeni adres kolu ise `ON CONFLICT` ile 0 satırdır.
`identity_claim_signup` bu yüzden **aynı adla, dördüncü argümanla** yeniden tanımlanır:
`(p_claim_hash text, p_tenant_id uuid, p_session_hash text DEFAULT NULL, p_verified_digest text DEFAULT
NULL)` → `TABLE(admin_user_id uuid)`. İmza değiştiği için `DROP` + `CREATE`; sahip, `SECURITY DEFINER`,
`SET search_path`, `REVOKE ALL … FROM PUBLIC`, `GRANT EXECUTE … TO tappa_app` yeniden yazılır (T5). Eski
binary'nin üç argümanlı çağrısı `DEFAULT` sayesinde yeni fonksiyona çözülür (expand,
`TestIdentityClaimSignup00080_TheOldBinarysCallStillClaims`); Down 00078'in tanımını ve ACL'ini bayt bayt
geri koyar (`TestIdentity00080_DownGivesBack00078sDefinitionByteForByte`). Gövde 00078'inkidir, iki ekle
(`TestIdentity00080_TheBodyIs00078sPlusThePasswordBranch`):
- **Tek kanıt:** oturum **ve** digest birlikte gelirse 0 satır.
- **Parola kolu** (`p_session_hash IS NULL AND p_verified_digest IS NOT NULL`): **karşılaştırma uygulamadadır** —
  `resolve_identity_by_email`'in döndürdüğü digest (yeni çözümleyici yok) ile yeniden girilen parola;
  Go'da tek üreticisi `adminauth.ProveWithIdentityPassword` olan `ClaimProof` o digest'i taşır (S12, yeni
  adres kolunun ve `identity_open_session`'ın güvencesiyle aynı tür). Definer (1) kimliği **talebin
  adresiyle** bulur (talebi kayıt sırrı buldu; R18 yapı gereği tutar), adresin ad alanlı kilidi altında
  (00074'ün anahtarı — S13); (2) kimliği `FOR UPDATE` ile yalnız `password_hash = p_verified_digest`
  **iken** kilitler — araya giren bir sıfırlama ya da parola değişimi READ COMMITTED'da yeniden
  değerlendirilir ve 0 satır olur (2026-10-23 kararının bu yoldaki karşılığı); (3) aynı kilit altında
  **aktif üyelik yok** şartını arar (`status = 'active' AND unlinked_at IS NULL`, `has_active_membership`'in
  yüklemi) — giriş yapabilen kişi canlı oturumla kanıtlar, parola tek başına oturum şartını **atlatamaz**;
  (4) tıklama posta kutusu kanıtıdır: `email_verified_at` boşsa dolar, `first_verify` penceresi ve
  `email_verified` olayı yazılır; owner satırı `linked_via='claim'`, kimliğin kayıtlı adresi ve digest'iyle
  doğar. 6b (ASCII adres kuralı) her koldan önce aynen koşar; Kelvin işaretiyle citext'te katlanan bir
  taslak adresi kimliği bulamaz.
- **Yeni GRANT yok:** kol yalnız 00071/00072/00078'in verdiği hücreleri kullanır; matris değişmez.
- **Kemerin dördüncü sınıfı** (`identityClaimAddressKeys`): bir `admin_identities` takma adı
  `<alias>.email OPERATOR(public.=) X` ile — X yazarlarının hepsi kemerden geçmiş bir **değişken**, burada
  kayıt token'ının bulduğu taslağın adresi — yalnız haritanın adlandırdığı fonksiyonda ve sayısıyla
  anahtarlanır (`identity_claim_signup`: 1). "Aktif üyelik yok" okuması kimlikle sınırlı adlı geniş
  okumadır (`identityTenantWideReads`: 1).
- **R18 değişmez:** kayıt yanıtı, zamanlaması ve mektup seçimi yeni / üyelikli bilinen / üyeliksiz bilinen
  adres için aynıdır; mektup yalnız "adreste kimlik var mı"ya göre seçilir, bilinen adresin tek mektubu
  iki kanıtı da anlatır ve kayıtlı yazıma gider. Onay sayfası (yalnız talep sırrını tutan görür) "sign in"
  ya da "Taptime parolanı yaz" der.
- **Bayrak:** kayıt doğrulaması bayrakla kapılanmaz (§8); bu kol da kapılanmaz.
- **§4.5 (3) metni değişmez:** kol kimliği kayıt token'ından bulur ve yazdığı her şey o token'ın
  taslağına bağlıdır; adlandırılmış istisna sayısı üç kalır.

**M14-14 notu (2026-10-26; kullanıcı onayı — AskUserQuestion, seçenek (A)):** `00075`,
`resolve_identity_by_email` ve `identity_consume_reset`'i `DROP` + `CREATE` ile yeniden tanımlar; ikisinin de
dönüşünün **sonuna** `email text` (kimliğin `admin_identities.email`'i, **kayıtlı yazımıyla**) eklenir;
gövdelerde başka tek değişiklik `identity_consume_reset`'in audit detail'inin biçimidir (aşağıda). **Neden:** 00075'ten önce hiçbir fonksiyon kimliğin adresini `tappa_app`'e
döndürmüyordu (kimlik tablolarında yetkisi yok), bu yüzden kartın iki maddesi karşılanamıyordu —
(1) **alıcı bağı** (M14-01 güvenlik denetiminin devrettiği not): `identity_open_reset` bir sırla değil
kimlik id'siyle anahtarlıdır ve linkin nereye gideceğini denetleyemez; link, ziyaretçinin yazdığı yazıma
(citext'te eşit, bayt bayt değil) değil kimliğin kayıtlı adresine gider ve linkin açıldığı id ile alıcı adres
**aynı** çözümleyici satırının iki sütunudur — Go'da tek bir tiple bağlanır (`adminauth.identityRecipient`,
tek kurucusu `recipientOf`; `TestIdentityRecipient_IsOneResolverRow`); (2) **EM-9 bildirimi** (§5.14,
kişi başına bir "parolanız değişti"): tüketim yalnız `(identity_id, revoked_sessions)` döndürüyordu ve
`tappa_app` bir kimlik id'sinden ne bir satıra ne bir adrese ulaşabilir. **md.7 (iii):**
`resolve_identity_by_email`'in yeni sütunu, çağıranın aradığı anahtarın kayıtlı yazımıdır — harf
büyüklüğü dışında çağıranın elinde olmayan bir şey taşımaz (ADR 0002 M14 notu). Tüketimin döndürdüğü adres,
harcanmış linkin gönderildiği posta kutusudur. **Yeni yetki yok:** `tappa_resolver` `admin_identities SELECT
(email)`'i 00071'den, `tappa_identitydefiner` 00072'den taşır (00075'in ön koşulu ikisini de doğrular).
Yeniden tanım kuralı (T5) aynen: `SECURITY DEFINER`, `SET search_path`, sahip, `REVOKE ALL … FROM PUBLIC`,
`GRANT EXECUTE … TO tappa_app` açıkça yeniden yazılır. Expand açısından güvenli: eski binary
`resolve_identity_by_email`'in sütunlarını adıyla seçer, `identity_consume_reset`'in eski binary'de çağıranı
yoktur. Down 00071/00072 tanımlarını bayt bayt geri koyar
(`TestIdentity00075_DownGivesBackTheOldDefinitionsByteForByte`; dev DB'de `pg_get_functiondef` ve `proacl`
md5'i 74 ↔ 75 → Down eşit). Kimlik başına linkin talep, teslim edilememe, "kept" ve reddedilme izi
**audit satırı değil** bütçeli bir log satırıdır — link hiçbir işletmenin değildir; işletmelerin izi
tüketimin `admin.recovery.completed` satırlarıdır. Bu satırların anahtarları **id'ler ve sabit değerli
anahtarlardır**, başka hiçbir şey: id'ler ADR 0022 §10'un kapalı kümesinden (`reset_id`, istek yolunda
`ip`, teslim hatasında `err_type` / `class` / `smtp_code`) ve §5.15'in `identity_id`'si; sabit değerliler
`outcome` (`ok` | `undelivered` | `kept` | `sent`), `reason` (`internal/handler`'ın sabit cümleleri),
`via` (`recovery`), `scope` (`identity` | `link`), reddin üç olgusu `already_used` / `superseded` /
`expired` (boolean) ve tamamlanmanın `revoked_sessions` sayısı. Adres, link, token, hash ve bunlardan
türeyen hiçbir değer yoktur (CLAUDE.md §7). **Tüketimin işletme satırının biçimi** (M14-14 güvenlik
denetimi): `identity_consume_reset` her işletmeye 00074'ün `identity_consume_row_reset`'in linkin kendi
işletmesi **olmayan** işletmelere yazdığı detail'i yazar — `outcome`, ve yalnız sıfır değilse
`revoked_sessions` (00072 onu hep yazıyordu; bir 0, satırı "kimlik başına link" diye ele verirdi);
`reset_id` **yok** — kimliğin linkinin id'si kişinin işletmelerini birbirine bağlardı
(`TestIdentityConsumeReset00075_TheDetailHasTheRowPathsShape`). **Bayrak bir kesme anahtarı değildir:**
tüketim bayraktan bağımsızdır (§8); canlı kimlik linklerini acilen iptal etmek runbook'un tek
satırıdır (`deploy/README.md`, M14-14).

**M14-18 notu (2026-10-27 kullanıcı kararı):** kapalı liste iki fonksiyonla büyür —
`identity_confirm_window` ve `identity_keep_all` (yukarıdaki iki satır; migration `00079`). Gerekçe
(M14-18'in STOP raporu, 2026-10-26): 00069–00075 onay penceresini **açıyor** ve üstünde **iş yapıyordu**
(çözme, geri alma), ama `tappa_app`'a pencereyi **okutan** (kapsam, bitiş, liste, reddedilmiş satırlar,
son-owner uyarısı — `linked_via` / `linked_at` / `unlinked_at` R16 gereği okunamaz) ve onu **kapatan**
("Keep all") bir fonksiyon yoktu; kartın onay listesi maddesi çizilemiyordu. **Yeni GRANT yok:** iki
gövde de definer'ın 00071/00072 hücrelerinde koşar (`admin_identities` SELECT/UPDATE pencere sütunları,
`admin_users` SELECT `role`/`status`/`linked_via`/`linked_at`/`unlinked_at`/`email`, `tenants` SELECT
`name`, `admin_identity_events` INSERT); 00079'un Down'ı bu yüzden hiçbir şey REVOKE etmez. Olay
kümesi (§5.1, `admin_identity_events_event_known`) bir değerle büyür: **`confirm_kept`** — "Keep all"ın
kabul edilen riskinin adlandırılmış hafifletmesi (§5.8 notu). İkisi de oturumla anahtarlıdır, yani §6.1
metnindeki "sırsız yazar" kümesine girmez; CLAUDE.md §4.5 (3) değişmez. İmza katalog testi, kemer
taraması (`identityTenantWideReads`, `identitySessionKeyLookups`, `identityEmailWideReads`) ve
"her yetki kullanılıyor" egzersizi aynı commit'te genişler.

**M14-18 denetim notu (2026-10-27 kullanıcı kararı, O1 seçenek (a)):** `00079`
`identity_unlink_backfilled`'ı **aynı imzayla** yeniden tanımlar: `first_verify` kapsamı `'account'`
bağlarını da listeler (`identity_confirm_window` aynı testi kullanır). Sebep: pencere açıkken yapılan
Undo (`identity_relink_refused`) satıra `linked_via='account'` yazar; `first_verify` onu listelemediği
için geri alınan satır listeden düşüyor ve bir daha çözülemiyordu. Bir `first_verify` penceresinde
`'account'` bir satır **yalnız bu pencerede yapılmış bir geri alma** olabilir: `'account'`'ın başka
yazarı yoktur ve sıfırlama `email_verified_at`'i doldurduğu için `first_verify` bir daha açılmaz. Gövde
00072'ninkidir, tek fark bu sabit; `SECURITY DEFINER`, `search_path`, sahip, `REVOKE ALL … FROM PUBLIC` ve
`GRANT EXECUTE … TO tappa_app` yeniden yazılır; Down 00072'nin gövdesini bayt bayt geri koyar. Aynı
denetimin **oran kovası** kararı: hesap eylemlerinin kovası bağlı oturumun **kendi** kişisidir; bağlanmamış
bir satır (önceden ekilmiş saldırganın satırı, §5.8) kişinin kovasını asla harcayamaz — doğrulama, çözme
ve geri alma ondan ücret almadan reddedilir, ters bağlamanın parola denemeleri kimliğin hiçbir eyleminin
kullanmadığı ayrı bir anahtarla (`reverse|…`) sınırlanır.

**M14-08 uygulama notları (2026-10-23; kart dışı kararlar, denetime açık):**
- `identity_create_for_signup(p_tenant_id uuid, p_admin_user_id uuid)` → **`boolean`** (bağladı mı)
  `00074`'te doğdu. Yüklemler tablodakilerin hepsi (`app.tenant_id` eşitliği, satırın bu tenant'ın,
  bağlanmamış, reddedilmemiş, aktif olması, `admin_users.created_at = now()` **ve** `xmin =
  pg_current_xact_id()::xid`, tenant'ın tek `admin_users` satırı) artı **`tenants.created_at = now()`**
  (işletme de bu transaction'da doğmuş). Dolu adres ve bcrypt biçimi INSERT'ten **önce** denetlenir:
  definer `password_hash`'i okuduğundan bir CHECK ihlali digest'i `DETAIL`'e yazardı. Yeni yetkiler:
  `admin_identities INSERT (email, password_hash)`, `admin_users SELECT (password_hash, xmin)`,
  `tenants SELECT (created_at)`. `pg_current_xact_id()::xid` 17.10'da ölçüldü: üst düzeyde doğru,
  SAVEPOINT altında yanlış.
- **Audit satırı yazmaz**: "bağlandı" satırı yalnız yeni adreste olurdu — §5.10'un bayrağın kapatmadığı
  hiçbir yüzeyde görünmesine izin vermediği fark. Backfill ve uzlaştırma da satırsız bağlar; dönüş
  değerini çağıran (kayıt) kimseye göstermez.
- Kemerin üçüncü sınıfı: `<alias>.email OPERATOR(public.=) <güvenilir değer>` ile sınırlı tek-adres
  okuması, fonksiyon başına sayıyla adlandırılır (yalnız bu fonksiyon, 1). `ON CONFLICT (email) DO
  NOTHING`'in üç sözcüğü bu gövde için adlandırılmış yapıdır.
- Kimlik üstünden parola değişimi ve eski sıfırlama linki, bağlı her işletmeye kendi
  `admin.password_changed` / `admin.recovery.completed` satırını **definer'ın kendisi** yazdığı için
  handler o yolda ikinci satır yazmaz (`adminauth.PasswordChange.Recorded`,
  `ConsumedReset.Recorded`). Satırın **biçimi** bağı ele vermesin diye (M14-08 denetimi) `00074`
  `identity_consume_row_reset`'i **aynı imzayla** yeniden tanımlar: linkin kendi işletmesine handler'ın
  satırı anahtar anahtar yazılır — `reset_id`, sıfır değilse `retired_count` (iptal edilen kardeş
  linkler) ve sıfır değilse `revoked_sessions` (`adminResetDetail`'in `omitempty`'si); Down `00072`'nin
  gövdesini geri koyar.
- Bayrağın tablosu (§8) `adminauth.IdentityGate`'tedir; HTTP yarısı `handler`'ın `mountGated`'i —
  bayrak başlangıçta okunduğu için kapalı bir yüzeyin rotaları **hiç bağlanmaz**: her yöntemde (GET,
  POST, HEAD, PUT, DELETE, OPTIONS, PATCH) panelin hiç sahip olmadığı bir yolun 404'ü bayt bayt döner.
  (İstek anında reddeden ilk biçim, r.Get/r.Post ile bağlı rotada öbür yöntemlere 405 + Allow
  döndürüyor ve yüzeyin varlığını sızdırıyordu — denetimde ölçüldü.)
- Adresin danışma kilidi (`hashtextextended('admin-identity-email:' || lower(email), 0)`) ve
  yazımı, başka bir işletmenin aynı adresli satırını kilit beklerken commit eden bir oturumla
  pinlenir: bekleyen kayıt kimlik doğurmaz (`ON CONFLICT` burada yerine geçemez — çakışacak kimlik yok).

**M14-08a notu (2026-10-26; kullanıcı kararı — §5.1'in adres kuralı):** `00076`
`identity_create_for_signup`'ı **aynı imzayla** yeniden tanımlar (`CREATE OR REPLACE`; sahip,
`SECURITY DEFINER`, `SET search_path`, `REVOKE ALL … FROM PUBLIC` ve `GRANT EXECUTE … TO tappa_app`
yine de açıkça yazılır — T5). Gövdede iki değişiklik, gerisi 00074'ün baytları:
- **Yeni yüklem (6b), adresin ad alanlı kilidinden önce:** satırın adresi `mail.ValidRecipient`'in
  SQL'e yazılmış hâlinden geçer — `octet_length ≤ 254`, `=?` yok ve net/mail addr-spec deseni
  (dot-atom `@` dot-atom ya da `net.ParseIP`'nin kabul ettiği IP alan literali), yazdırılabilir ASCII'de.
  Tutmazsa `false`, hiçbir şey yazılmaz; `IS NOT TRUE` NULL'u da reddeder. Desen ile `ValidRecipient`
  bir tablo (U+0130, U+212A, punycode, boşluk, denetim karakterleri, tırnak, noktalar, her literal biçimi)
  ve üretilmiş bir külliyat üzerinde aynı cevabı verir (`TestIdentityAddressRule_SQLAgreesWithValidRecipient`).
- **Adlandırılmış e-posta-geniş okuma** yazdırılabilir ASCII olmayan bir satırı artık **saymaz** (bir
  FOR döngüsü; desen plpgsql'de karar verilir, kemerin modeli bozulmaz, okuma sayısı yine 1). Yoksa eski
  binary'nin yazdığı bir homoglif satırı, gerçek adresin sahibinin kayıtta kimlik doğurmasını
  engellerdi. Bu kümede yazdırılabilir ASCII tam kuraldır: 6b'den geçmiş bir adrese citext'te eşit
  bir ASCII adres ondan yalnız harf büyüklüğüyle ayrılır ve kural harf büyüklüğünü görmez.
- **Uygulama sınırı:** `signup.ValidateAccount` adresi aynı fonksiyondan (`mail.ValidRecipient`, kopya
  değil) geçirir; ASCII olmayan adrese kendi cümlesi. Giriş ve sıfırlama formları yalnız **arar**,
  değişmedi (düz zamanlama korunur; homoglifle yapılan bir giriş en çok gerçek kimliğe çözülür ve onun
  parolasını ister).
- **Bundan sonra doğan her kimlik INSERT'i** (`identity_reconcile`, `identity_accept_invite_new`,
  `identity_claim_signup`) aynı kuralı INSERT'ten önce bir ret olarak taşır. CHECK'in yerine geçen
  kemer yapısaldır ve kapalı başarısız olur (`TestIdentityAddressRule_EveryIdentityInsertDecidesIt`):
  sahibi `admin_identities`'e INSERT edebilen (sütun yetkisi yeter) ve tanımı (`pg_get_functiondef` —
  `BEGIN ATOMIC` gövdesi dahil) tabloyu herhangi bir yazımla anan ya da dinamik SQL (`EXECUTE`) koşan
  her fonksiyon, kuralı yorumlar ayıklandıktan sonra gerçek bir `IF (…) IS NOT TRUE THEN RETURN |
  CONTINUE` reddi olarak taşımalı ya da adıyla listelenmiş, oraya gerçekten yazmayan bir okuyucu
  olmalıdır (bugün on dört `identity_*` fonksiyonu; `resolve_identity_by_email`'in sahibi INSERT
  taşımadığı için aday değil); tabloyu hedefleyen rewrite kuralı sayısı 0'dır. Kemer **varlığı**
  denetler; reddin eklenen değere uygulandığı her yazarın kendi davranış testidir. `00076`'nın Up'ı
  ayrıca yazdırılabilir ASCII olmayan adresli bir kimlik görürse yalnız sayıyla durur (ön kontrol
  ile `00073` arasındaki pencere).
- **Kapsam dışı kalan (açık madde):** `identity_link_legacy` adresleri citext ile karşılaştırır ve bu
  notla değişmedi. ASCII olmayan bir satırı ancak iki taze oturumu — satırın ve kimliğin parolasını —
  birlikte kanıtlayan kişi kendi kimliğine bağlayabilir; bu bir gasp yolu değildir, ama kuralın
  harfiyle ("hiç bağlanmaz") uyum için aynı yüklem oraya da gerekir — ayrı bir kart.

**M14-15 uygulama notları (2026-10-26/27; kart dışı kararlar ve iki kullanıcı kararı, denetime açık):**
- İmzalar (`00077`): `resolve_admin_invite_by_token_hash(p_token_hash text)` → `TABLE (id, tenant_id, role,
  expires_at, used_at, cancelled_at)` (argüman adı `resolve_identity_reset_by_token_hash`'inki gibi
  `p_token_hash`; tablo ikisini de `p_hash` diye kısaltır); `identity_accept_invite(p_invite_hash text,
  p_session_hash text)` ve `identity_accept_invite_new(p_invite_hash text, p_new_hash text)` → `TABLE
  (admin_user_id uuid, tenant_id uuid)` (doğan ya da yeniden bağlanan üyelik ve işletmesi; red 0 satır).
- `admin_invites`'in sütun özetine `full_name` eklendi (yeni üyelik satırının `NOT NULL` adı — owner'ın
  yazdığı ad); `role` CHECK'le yalnız `manager` (sahiplik M14-16'nın iki adımlı devriyle döner, e-posta
  linkiyle değil); adres başına tek açık davet (kısmi UNIQUE) — yeni davet açık olanı iptal eder.
  Definer matrisi: `admin_invites` SELECT (token_hash dahil dokuz sütun) + UPDATE (`used_at`),
  `admin_users` INSERT (matrisin sekiz sütunu; `00077` bu yetkinin sahibidir — Down'ı onu yalnız
  `identity_claim_signup` yokken geri alır, M14-17'nin `00078`'i almaz). `tappa_app`: `token_hash`
  dışında SELECT, INSERT, yalnız `UPDATE (cancelled_at)`, DELETE yok.
- **Reddedilmiş satır yalnız davetin rolündeyse geri bağlanır** (iki denetimin YÜKSEK bulgusu, 2026-10-27):
  `linked_via='invite'`, **yeni** `linked_at`, `unlinked_at = NULL`, digest kopyası, `status='active'`;
  yüklem `(identity_id IS NULL AND unlinked_at IS NOT NULL AND role = davetin rolü) IS NOT TRUE → 0 satır`.
  Reddedilmiş bir **owner** satırı manager davetiyle hiçbir zaman geri gelmez (definer'da `UPDATE (role)`
  yok; owner koltuğu M14-16'nın devridir) — kabul 0 satır, davet açık, yanıt tek tip. İşletmede adresin
  başka bir satırı varsa da kabul 0 satır ve davet açık kalır: yanıt tıklayana gider, owner yok sayılmış
  bir e-postanın gördüğünü görür (R18). Yeni üyelik satırına iki yolda da **davetin yazımı** yazılır.
  Yeni kimlik `email_verified_at = now()` ile doğar (INSERT sonra UPDATE — definer'a `INSERT
  (email_verified_at)` verilmez); onay penceresi açmaz.
- **Askıdaki işletme yeni yönetici almaz** (kullanıcı kararı, 2026-10-27): iki kabul fonksiyonu kilitlerden
  sonra `tenants.suspended_at`'i okur; doluysa ya da satır yoksa hiçbir şey yazmaz, davet açık kalır,
  sayfa "şimdi kabul edilemez" der. Davetlinin sayfası (`/admin/invite`) panelin yazma rotalarından değil,
  sıfırlama akışı gibi kendi bağlamasıyla bağlanır; askı istisna listesi 13'te kalır (ADR 0025'in M14 notu).
- **ASCII adres kuralı**: davet adresi kaydedilmeden önce `mail.ValidRecipient`'ten geçer (form doğrulaması,
  her okumadan önce — R18'i bozmaz) ve `admin_invites_email_ascii` CHECK'i yalnız yazdırılabilir ASCII'yi
  saklar; `identity_accept_invite_new` M14-08a'nın 6b yüklemini **birebir** taşır (yukarıdaki not);
  `identity_accept_invite` iki adresten biri ASCII değilse bağlamaz. `00077`'nin ön koşulu `00076`'dır.
- Kemer: `admin_invites` kendi küresel UNIQUE `token_hash`'iyle bir tenant ilişkisini çapalar
  (`identityTokenKeyed`); `resolve_admin_invite_*` gövdeleri de kemerde.
- **Owner kapısı politika motorundandır** (`tenant.RuleWriter.MayInviteManager`): `policy:edit`; yeni bir
  eylem kapalı sözlüğü (ADR 0004 §8) büyütür ve materyalize edilmiş baseline'da `deny sid=default` alırdı
  (M7-07'nin tuzağı). Saklı kural kitabı **hiç yoksa** (hiç materyalize edilmemiş işletme) karar
  **sevk edilen baseline**'la verilir (kullanıcı kararı, 2026-10-27) — bu yolda yazma yok;
  materyalize edilmiş işletme saklı kitabıyla karar verir. İkinci kemer: davet INSERT'i davet edenin aktif
  owner satırından yazılır.
- Gönderme `mountWriting`'in askı halkalı ana grubunda (ADR 0025'in kapalı listesi daveti adlandırmaz).
- Davet linkinin token'ı adres çubuğunda kalmaz: GET onu dar yollu bir çereze koyar ve temiz
  `/admin/invite`'a 303 verir (sıfırlama akışının deseni).
- Davet ve kabul audit satırları: `admin.invite.sent` / `admin.invite.undelivered` (handler, gönderimin
  sonucuyla, `invite_id`), `admin.invite.cancelled`, `admin.invite.accepted` (definer, `{outcome,
  invite_id}`, iki fonksiyonda aynı biçim). Adres, ad, token yok.

Her fonksiyon: `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, şema-nitelikli gövde,
dinamik SQL yok, sabit dönüş sütunları, `REVOKE ALL … FROM PUBLIC`, `EXECUTE` **yalnız**
`tappa_app` — **tek istisna `identity_reconcile()`**: `EXECUTE` yalnız `tappa_owner`'da (katalog testinin
izin listesi bunu adıyla taşır — ONAY notu 1); `tappa_operator` ve `tappa_opdefiner` hiçbir `identity_*` / `resolve_identity_*`
fonksiyonunu çalıştıramaz ve hiçbir kimlik tablosunda yetki taşımaz (katalog testi
`has_function_privilege` / `has_any_column_privilege`). Tenant'a yazan her fonksiyon **o tenant'ın**
`audit_log`'una yazar ve satır karşı tenant'ı anmaz (istisna: `identity_create_for_signup`, M14-08 notu).

**Kanonik liste ve matris** (R9). Yukarıdaki tablo bu ADR'nin onayladığı listenin **tamamıdır**; her
kart kendi satırlarını ekler ve bu ADR o eklemeleri **önceden onaylar** — listede olmayan bir fonksiyon
kullanıcı onayı + ADR değişikliği ister. Kanonik biçim M14-04'te doğan **imza tablosudur**
(`pg_get_function_identity_arguments` + `pg_get_function_result` metni) ve ADR 0027 §2/§3'teki gibi
katalog testiyle birebir pinlenir. `tappa_identitydefiner`'ın **fiil × sütun matrisi** (özet; tam hâli
M14-04'ün testinde): beş kimlik tablosunda gerektiği kadar — `admin_identities`: SELECT (`id`, `password_hash`,
`last_used_tenant`), UPDATE (`last_used_tenant`, `last_login_at`) — M14-04; `password_hash` yalnız
`identity_open_session`'ın ve (M14-17b, 2026-10-27) `identity_claim_signup`'ın parola kolunun kilit altındaki
karşılaştırması için okunur, hiçbir fonksiyon döndürmez —
`identity_password_for_session` dışında, o da yalnız canlı oturumla (hesap sayfasının mevcut parola
denetimi) (2026-10-23); INSERT (`email`, `password_hash`) — `identity_create_for_signup` (M14-08);
`admin_identity_resets`: SELECT (`id`,
`identity_id`, `token_hash`, `expires_at`, `used_at`, `cancelled_at`), INSERT (`identity_id`, `token_hash`,
`expires_at`) — `identity_open_reset`, UPDATE (`used_at`, `cancelled_at`) — tüketim ve
`identity_withdraw_reset`; `admin_identity_events`: INSERT (`identity_id`, `event`, `detail`), SELECT (`created_at`) ve DELETE
(yalnız `identity_sweep_events`); `admin_signup_claims`'in hücrelerini — `identity_sweep_signup_claims`'in
SELECT/DELETE'i dahil (süresi dolmuş ya da kullanılmış satırlar, §5.1) — M14-17 ekler · `admin_users`: SELECT (`id`, `tenant_id`,
`email`, `password_hash`, `role`, `status`, `identity_id`, `linked_via`, `linked_at`, `unlinked_at`,
`created_at`) ve sistem sütunu `xmin` (`identity_create_for_signup`, M14-08), INSERT (`tenant_id`, `full_name`, `email`, `password_hash`, `role`, `identity_id`,
`linked_via`, `linked_at`), UPDATE (`password_hash`, `identity_id`, `linked_via`, `linked_at`,
`unlinked_at`, `last_login_at`, `status` — yalnız contract sonrası çözmenin devre dışı bırakma biçimi,
T1) — **`UPDATE (email)` yok**: e-posta değiştirme K-15 ile kapsam dışı,
gelirse kendi genişlemesini getirir (S11) · `password_resets`: SELECT (`id`, `tenant_id`,
`admin_user_id`, `token_hash`, `expires_at`, `used_at`, `cancelled_at`), UPDATE (`used_at`,
`cancelled_at`) — eski link tüketimi (S10) ve çözmede iptal (T5) ·
`admin_sessions`: SELECT, INSERT (`tenant_id`, `admin_user_id`, `token_hash`, `authenticated_at`),
UPDATE (`revoked_at`) · `tenants`: SELECT (`id`, `name`, `vat_number`, `suspended_at`, `created_at`)
ve sistem sütunu `xmin` (S2, T7) — **INSERT yok**, tenant'ı uygulama yaratır · `audit_log`: INSERT · `admin_invites`: SELECT, UPDATE (`used_at`). Contract B
`tappa_app`'tan `admin_users` `INSERT (email, password_hash)`'i aldıktan sonra **owner ve davetli
satırlarını yalnız definer** yazar (`identity_claim_signup`, `identity_accept_invite*`).

**`tappa_resolver`'ın yetkileri** (T6; katalog testi pinler): `admin_users` SELECT (`id`, `tenant_id`,
`email`, `password_hash`, `status`, `created_at` — bugünkü, `00011:212`, `00017:346`) + **yeni**
(`identity_id`, `unlinked_at`) — `resolve_admin_by_email`'in süzgeci ve dönüşü,
`resolve_identity_by_email`'in `has_active_membership`'i için; `admin_identities` ve beş çözümleyicinin
okuduğu token tablolarında yalnız döndürdükleri ve süzdükleri sütunlar.

**Yeniden tanımlanan her fonksiyon** (T5) — `resolve_admin_by_email` (`DROP` + `CREATE`, dönüş tipi
değişir) ve contract B'nin `CREATE OR REPLACE`'leri — `SECURITY DEFINER`, `SET search_path = pg_catalog,
pg_temp`, sahibi, `REVOKE ALL … FROM PUBLIC` ve `GRANT EXECUTE … TO tappa_app`'i **açıkça yeniden
yazar** (00017 `DROP` + `CREATE` kullandı ve yetkiler onunla düştü); katalog testi `prosecdef`,
`proconfig` ve `tappa_app` EXECUTE'unu her yeniden tanımdan sonra pinler.

**Uzlaştırma bir bakım fonksiyonudur** (T3, T8): `identity_reconcile()` — sahibi
`tappa_identitydefiner`, `EXECUTE` **yalnız `tappa_owner`'da** (`tappa_app`'ta yok); M14-06b'nin
migration'ı onu yaratır ve çağırır, runbook M14-19'dan önce yeniden çağırır, contract B düşürür. Uygulama
canlıyken koştuğu için kimlik INSERT'i aynı ad alanlı e-posta kilidini ve `ON CONFLICT DO NOTHING`'i
kullanır.

### 5.3 İstek başına yol

Çerez → `resolve_admin_session_by_token_hash` → `WithTenant(rs.TenantID)` → `TouchAdminSession`.
`admin_identities`'e istek başına okuma **yok**. `TouchAdminSession`'ın `RETURNING`'ine iki sütun
eklenir — `tenants.name` (kabukta işletme adı, ek okuma yok) ve `admin_users.identity_id` (geçiş oran
kovası). Askı kapısı, `ProtectWriting`, `RequireAdmin`, ~50 `AdminOf` çağrı yeri ve rol kapıları
dokunulmadan çalışır; var olan oturumlar deploy'u atlatır.

**M14-12 uygulama notu (2026-10-26):** ad `adminauth.Resolved.TenantName`'den (aynı `RETURNING`; Verify
işletme işleminde tek ifade koşar — `TestVerify_CarriesTheBusinessNameTheStatementReturned`). Markasız
işletmede kabuk Wordmark'ın altına bir işletme bloğu çizer (ad; bayrak `on` iken "Switch business"
bağlantısı); markalıda ad zaten başlıkta olduğundan blok yalnız bağlantıdır, bayrak `off` iken markalı
kabuk bayt-aynıdır. **Bağlantı bayrak `on` iken her zaman görünür** — ölçülen seçim: üyelik sayısını
damgalamanın yeri yok (`admin_sessions`'ta sütun yok, `tappa_app`'ın INSERT'i yalnız `id, tenant_id,
admin_user_id, token_hash`; `identity_open_session`/`identity_switch_session` sayı döndürmez — 00071),
yani damga bir migration + iki definer yeniden tanımı ister ve başka işletmede kabul edilen bir davet ya
da kaldırılan bir üyelikle bayatlar; tek işletmede sayfa bunu söyler (M14-13). WL-8 panel golden'ı
(`TestPanelBrand_AnUnbrandedChromeIsTheChromeBeforeWL8`) değişmedi — derlemi işletme taşımayan kabuktur;
adlı kabuğun golden'dan yalnız blok kadar farklı olduğunu `TestPanelShell_TheNamedShellIsTheGoldenPlusTheBlock`
ölçer.

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

**M14-04 uygulama notları (2026-10-22, kabul edilmiş):** hedef mevcut tenant'sa 0 satır (geçiş yok);
geçiş hedef üyeliğin `last_login_at`'ini damgalar.

**Eşzamanlı geçiş** (bulgu 29): aynı oturumla N paralel istek → tam olarak bir kazanan; kaybedenlerin
yanıtı **`Set-Cookie` yazmaz ve çerezi silmez** (kazananın çerezi tarayıcıda kalır).

**Oran sınırı** (bulgu 8): kova `identity_id`'ye (+ kaynak adres) anahtarlı — oturum id'sine değil,
çünkü her geçiş yeni oturumdur ve oturum kovası sıfırlanırdı. Aşılınca 429, audit yazılmaz.

**M14-10 uygulama notları (2026-10-26):**

1. **Senkronizör:** `HMAC(K, "tappa/admin-switch/v1|" + session_id)`, `K = HMAC(HMAC(TAPPA_SESSION_HMAC_KEY,
   "tappa/admin-switch/v1/key-derivation"), ham oturum token'ı)` — §5.5'in bağıyla aynı şekil, kendi
   etiketiyle (`internal/adminauth/switchtoken.go`, sabit-zamanlı karşılaştırma `cmd/tappa`'nın envanterinde).
   Formun gövdesinde (`csrf`); bağ (`?tb=`) ayrıca action'da. Sıra: zincir → senkronizör → oran kovası →
   definer. Senkronizör reddi 400, kova 429, hedef reddi **tek tip 403** (sayfa gövdesi her nedende bayt-aynı);
   hiçbir ret `Set-Cookie` yazmaz.
2. **İki kova, ikisi de her denemede (senkronizörden sonra) yüklenir; biri dolarsa 429:**
   - **(kişi, adres) çifti**, 30 / 10 dk (`adminSwitchLimit`, pinli). Bir adres bu kovadan yalnız kendi
     30'unu harcar. Ama her ret yüklendiği için (depo kuralı: sınırlayıcının kendi reddi de sayılır), kişinin
     çerezini tutan biri **tek adresten** aşağıdaki kişi kovasını (120) tüketebilir; bunun kişiye bedeli en
     çok 10 dakikalık geçiş kaybıdır — çıkış + yeniden giriş bu sürede de açıktır.
   - **Yalnız kişi**, gevşek: 120 / 10 dk (`adminSwitchPersonLimit`, pinli; M14-10 güvenlik denetimi). Çift
     kovanın adresler arası tavanı yoktu — N adres 30 × N geçiş, her biri silinmeyen dört satır yazardı. Bu
     kova adres ne olursa olsun kişiyi sınırlar (pencere başına en çok 480 satır); dört adreslik pay kadar
     gevşek olduğu için çalınmış bir çerez kişiye en çok birkaç dakikalık geçiş kaybettirir, paneli değil.
   Kişi değeri `identity_id`'nin kendisi değil, `internal/adminauth` içinde hesaplanan opak bir MAC'tir
   (`Resolved.SwitchBucket`) — kimlik id'si paketten çıkmaz. Adres başına toplamı ayrıca `adminFloodLimit`
   sınırlar. Bağlanmamış satırın kişisi yoktur; onun geçişi zaten 0 satırdır ve kovası oturumudur. Sıra
   (senkronizör → kovalar) testle pinli: sahte POST'lar kovayı harcamaz.
3. **`GET /admin/switch` bu kartta en küçük hâliyle** (giriş ailesinin çerçevesi, mevcut işletme, öbür
   üyeliklerin her biri için bir form); sayfanın tasarımı M14-13'ündür. Liste `identity_memberships`'ten
   (`Manager.Memberships`, aynı token anahtarıyla) gelir ve geçişin yetkisi değildir.

**Askı:** hedef askıdaysa geçiş serbest (ADR 0025 `:509-514` — giriş ve okuma açık); panel o
işletmede salt okunur. ADR 0025'in sekiz rotalık istisna listesi (`:374-387`) **birden fazla** rota
büyür (bulgu 16): `/admin/switch`, "Sign out everywhere", "Link a business", e-posta doğrulama
talebi — hepsi `protectWritingThroughSuspension` grubunda (kendi parolası rotasının yanında).
M14-01'in ADR 0025 notu bu listeyi yazar.

### 5.5 Eski sekmeden yanlış işletmeye yazma — tenant'a bağlı formlar (bulgu 3, R7)

Bütün panel tek çerez, tek yol (`Path=/admin`); A'da açık kalmış bir sekmenin formu, B'ye geçildikten
sonra gönderilirse **B'ye yazardı** — bugün panel yazmalarında tenant'a bağlı bir senkronizör yok
(`adminlogin.go:742-800`). Kapanış (M14-10a, M14-10'dan **önce**):

- **Bağ gövdede değil, formun `action` URL'sinin sorgu parametresindedir** (`?tb=…`). Sebep (R7):
  kapı bir **gövde** alanını okusaydı, gövdeyi her handler'ın kendi `http.MaxBytesReader` sınırından
  **önce** varsayılan 10/32 MB sınırla ayrıştırırdı — bugün handler'lar sınırı ayrıştırmadan önce koyar
  (`billingactions.go:281-296`, `brandactions.go:339`, `:416`, `accountactions.go:109`; çok parçalı logo
  yüklemesi `brandupload.go:42`, `:321`). Sorgu parametresi `r.URL`'dan okunur, gövdeye dokunmaz.
  Paneldeki formlar JS'siz düz HTML'dir, başlık taşıyamaz; htmx yalnız GET taşır
  (`web/templates/pages/transactions.templ:601`).
- **Değer:** `HMAC(K, "tappa/admin-form-tenant/v1|" + tenant_id)`, `K` = **ham oturum token'ından**
  (çerez değerinden) türetilmiş anahtar — `token_hash`'ten **asla** (o veritabanında okunabilir). Böylece
  bağ aynı zamanda gerçek bir **senkronizör token**'dır: oturumu bilmeyen üretemez. Geçiş yeni token
  verdiği için eski sekmenin bağı yeni oturumda doğrulanmaz → 409.
- `ProtectWriting` ve `protectWritingThroughSuspension` herhangi bir yazmadan önce bağı oturumun
  tenant'ı ve token'ıyla karşılaştırır; uyuşmazsa 409 ve *"This page belongs to another business —
  reload it."* **Anahtar rotasyonu** (`TAPPA_SESSION_HMAC_KEY`) açık her formu 409'a düşürür —
  adlandırılmış bedel.
- Bağ **yalnız reddetmek** için okunur; bağlam asla ondan kurulmaz (§3).
- **Bağ hiçbir yanıt URL'sinde kalmaz** (S14): uygulama log'u yalnız rota desenini yazar, URL'yi değil
  (`internal/httpx/requestlog.go:436-452`), panel `no-referrer` gönderir (`adminlogin.go:1763-1768`);
  ek olarak her bağlı POST **POST-redirect-GET** ile biter (yönlendirme hedefinde `tb` yok — kabul
  kriteri) ve runbook ingress erişim log'unun kapalı ya da sorgusuz olduğunu doğrular.

**M14-10a uygulama notları (2026-10-26, kullanıcı kararı — üçü de önerilen seçenek):**

1. **Plaket encode rölesinin üç rotası bağdan muaftır** (`/admin/plaques/encode`, `…/step`, `…/abort`;
   kapalı küme `encodeRelayRoutes`, `internal/handler/formbinding.go`; testte adıyla:
   `TestFormBinding_EveryWritingRouteRefusesAPostWithoutItsBinding`). Sebep: bunlar form değil. Android
   rölesi (`android/`, ADR 0017) sayfa çizmez, panel çerezini her istekte WebView'ın `CookieManager`'ından
   okur — eski bir sayfası yoktur ve bağı öğrenemez; bağlansalardı plaket encode tamamen dururdu. Onları
   koruyan zincir değişmedi: `floodGate` (adres tavanı) → `sameOriginGate` (Origin, çözümleyiciden
   **önce**) → `requireAdmin` (oturum çerezi → `resolve_admin_session_by_token_hash`) → `sessionGate`
   (oturum bütçesi) → `suspensionGate` (askıda JSON `suspended`) → `encodeGate` (oturum başına encode
   tavanı + 8 KiB gövde sınırı); tur kimliği `Begin`'de oturumun tenant'ına bağlanır. Röle için bir bağ
   sözleşmesi (rölenin bağı sunucudan alıp taşıması) **ileride açılabilecek ayrı bir iştir**.
2. **Sayfayla cevap veren bağlı POST'lar `?tb=`'yi adres çubuğunda ve geçmişte bırakır** — doğrulama
   hatasında yeniden çizilen form, inceleme/onay adımları, bir kez gösterilen anahtar ve link ekranları.
   **Kabul edildi:** bağ o oturumun ham çerezi olmadan işe yaramaz — çerezi bilmeyen onu ne üretebilir
   ne de tek başına yeniden oynatabilir (kapı çerezi ve Origin'i ayrıca ister). POST-redirect-GET
   yönlendirmeyle biten her yazmada geçerlidir: hiçbir yönlendirme hedefi `tb` taşımaz (kapı bağı
   handler'ın gördüğü URL'den siler; `TestFormBinding_NoRedirectCarriesTheBinding`,
   `TestFormBinding_TheHandlerSeesNoBindingAndNoNewTenant`).
3. **Ingress erişim log'u panel yollarında kapalı.** Ölçüm (repodan): denetleyicinin ConfigMap'i boş, yani
   erişim log'u açık ve varsayılan biçim `"$request"`'i (sorgu dahil) yazar. `deploy/k8s/40-ingress.yaml`'a
   ayrı bir Ingress (`tappa-panel`) eklendi: aynı host, TLS, sınıf, backend ve notlar, tek yol `/admin`
   (Prefix — panel çerezinin `Path`'i, `adminauth.CookiePath`) ve
   `nginx.ingress.kubernetes.io/enable-access-log: "false"`; cert-manager issuer notu yalnız `tappa`'da
   (tek sır, tek Certificate). ingress-nginx en uzun öneki seçtiği için `/admin` ve altı yalnız
   `tappa-panel`'e gider; geri kalan her şey `tappa`'da ve log'lu kalır. Pin:
   `TestPackaging_ThePanelPathsAreRoutedWithoutAnAccessLog`. Doğrulama komutu runbook'ta
   (`deploy/README.md`, *"Panel formlarının tenant bağı (M14-10a)"*). **Kabul edilen bedel
   (denetim, 2026-10-26):** not yalnız **erişim** log'unu kapatır; denetleyicinin **hata** log'u nginx'in
   kendisinin düşürdüğü bir panel isteğinde (1m'yi aşan gövdenin 413'ü, upstream bağlantı hatası, zaman
   aşımı) istek satırını sorgusuyla birlikte yazmaya devam eder. Gerekçe not 2'ninki: bağ o oturumun ham
   çerezi olmadan işe yaramaz. Yapılandırma değişmez.

### 5.6 Giriş

Bayrak açıkken: e-posta → `resolve_identity_by_email` (≤ 1) → kimlik digest'i karşılaştırılır →
`identity_open_session` → `/admin`; iniş `last_used_tenant`, değilse en eski aktif üyelik (K-4).

**Geçiş dönemi eski kolu:** aynı e-postalı **bağlanmamış** ve `unlinked_at IS NULL` satırlar bugünkü
gibi (en çok 8, incumbent-first) karşılaştırılır. Bağlanmamışlık Go'da ayrılır (T2): yeniden tanımlanan
`resolve_admin_by_email` `identity_id`'yi döndürür ve kimlik yolu kimliğin **kendi** satırlarını adaylardan
çıkarır — böylece bağlı satırlar 8'lik pencereyi doldurmaz. **Zamanlama beş durumda da düz** (bulgu 10, R14):
kimlik var/yok × bağlanmamış satır var/yok (bilinmeyen e-posta dahil) **ve** doğru parola + bütün
üyelikleri devre dışı kimlik. Her giriş contract'a dek **tam 1 + `MaxCandidates`** karşılaştırma öder;
`has_active_membership` false ise uygulama `identity_open_session` yerine bir sahte çağrı yapar. Ölçü
gidiş-dönüş **sayısı değil süredir** (S15): sahte çağrı yazan `identity_open_session` kadar sürmelidir
(aynı biçimde bir yazma + geri alma), uygulama bcrypt'i `has_active_membership`'e göre **asla** atlamaz
ya da öne çekmez (dal karşılaştırmadan **sonra** seçilir), ve bilinen e-postada olay satırının yazılması
beş durumlu zamanlama testinin **içindedir** (bugünkü `failLogin` deseni, `adminlogin.go:1288-1333`). `adminLoginWorkLimit` (`adminratelimit.go:737`) 9 karşılaştırmaya göre yeniden
boyutlanır. Yazılan parola hem kimliği hem bir eski satırı doğrularsa: önce eski satırda, sonra
kimlikte **taze** bir oturum açılır, `identity_link_legacy` iki oturumun hash'iyle çağrılır, eski oturum
iptal edilir (`linked_via='password'`). Yalnız eski satır(lar) doğrularsa kişi bugünkü gibi kimliksiz
girer — **birden çok eski satır doğrularsa seçici contract'a dek kalır** (K-4'ün "seçici yok" ifadesi
yalnız kimlik yoluna aittir; bulgu 10).

Başarısız kimlik girişleri `identity_record_failed_login` ile `admin_identity_events`'e yazılır
(kimlik başına kova, aşınca tek `FirstOverLimit` satırı). Tenant audit satırlarından kişinin öbür
işletmeleri hakkında bilgi taşıyan her alan kimlik yolunda **kalkar** (R21): doğrulanan işletme sayısı
`VerifiedBusiness` (`adminlogin.go:1125`, `:1204`), `ActionAdminLoginTruncated`'in `Resolved` /
`Compared` sayıları (`:1555-1562`) ve ret satırındaki `AttemptedTenant` (`:1203`, `:1547-1549`).
`adminAccountLimit` (`adminratelimit.go:740-781`) kimlik yolunda `identity_id`'ye anahtarlanır.

**M14-07 uygulama notu (2026-10-23; denetimlerin kabul ettiği kart dışı kararlar):**
- **Sahte çağrı (decoy)** bir commit kaydı turu + bekleme olarak uygulandı: `SELECT pg_current_xact_id()`
  (satır yazmaz; başka WAL yazmadığı için commit'i diske **flush edilmez**), ardından sürecin son gerçek
  `identity_open_session` açılışlarının medyanına (en çok 250 ms) kadar beklenir; bekleme bir son tarihe
  kadar uyur, son milisaniyeyi `runtime.Gosched` ile geçirir (yüklü CI'da düz uyku 1.57x/1.60x geç kaldı).
  Yukarıdaki "aynı biçimde bir yazma + geri alma" birebir uygulanmadı, çünkü `tappa_app`'ın yazabildiği
  her tablo tenant kapsamlıdır: açılışın işini gerçek bir şey yazmadan yeniden üretecek ifade yoktur.
  Migration'lı bir decoy fonksiyonu **reddedildi**: kapalı listeyi güvenlik yükü taşımayan bir özellik
  için genişletirdi. Beş başarısızlık durumu **eşit işten** düzdür (her biri aynı iki çözümleyiciyi, 1 + 8
  karşılaştırmayı, aynı decoy'u ve aynı olay çağrısını öder); bekleme yalnız başarısızlığın açılış adımını
  başarılınınki kadar uzatır, başarı ise yanıttan zaten görünür. Bekleme durumlar arasında bir şey
  sızdırmaz: her başarısızlık aynı hedefe bekler.
- **`CreateAdminSession`** yüklemi ve satır kilidi (`FOR NO KEY UPDATE OF a`, §5.2) bugünkü girişte de
  bayraktan bağımsız geçerlidir: çözme bayrağın açık olduğu bir pod'dan, giriş kapalı olandan gelebilir.
  `Issue`'ya doğrulanan digest bir **Proof** ile ulaşır (doğrulanan digest'in, oturum anahtarından
  türetilmiş anahtarla MAC'i; seçici çerezi onu imzalı taşır, `Issue` satırın güncel digest'iyle sabit
  zamanlı karşılaştırır). Seçici blob'unun sürümü "2" oldu; rollout sırasında eski bir blob "yeniden
  giriş" ister.

**M14-09 uygulama notu (2026-10-26; `4ccc6f69` … `5a45f7d6`, iki denetimin kabul ettiği biçim):**
- **İniş.** Bayrak açıkken `Login` kimlik yoluna geçer (`adminlogin.go:1069-1070` → `loginAsPerson`).
  Giriş formu işletme adlandırmaz, bu yüzden `AuthenticatePerson` `uuid.Nil` ile çağrılır
  (`adminloginidentity.go:68-72`); `db.OpenIdentitySession` bunu `NULL` istenen işletmeye çevirir
  (`internal/db/identity.go:133-136`) ve `identity_open_session` son kullanılan işletmeye
  (`last_used_tenant`) iner; böyle bir işletme yoksa ya da oradaki üyelik aktif değilse en eski aktif üyeliğe
  (`00071_resolve_identities_and_switch_sessions.sql:209-249`); indiği işletmeyi `last_used_tenant`'a
  yazar (`:260-263`). Kimlik yolunda seçici yoktur; birden çok **eski** satır doğrularsa seçici
  contract'a dek kalır. Başarı satırını `identity_open_session` yazar, handler ikincisini **yazmaz**
  (`adminloginidentity.go:104-106`).
- **R21.** Kimlik yolunda giriş akışının, kişinin öbür işletmeleri hakkında bir sayı ya da bir işletme
  taşıyabilen her audit detail'i `loginDetail`'den geçer (kodun kendi sınırlaması,
  `adminloginidentity.go:169-191`; çağrı yerleri `adminlogin.go:1194`, `:1273`, `:1543`). Bunu taşıyamayan
  sabit biçimli satırlar doğrudan yazılır (ör. `adminlogin.go:1334`, `:1391`, `:1418`,
  `adminloginidentity.go:118`, `:148`). `loginDetail` `VerifiedBusiness`,
  `Resolved`, `Compared` ve `AttemptedTenant`'ı sıfırlar. Seçicinin satırları da buna dahildir, çünkü o yolda
  seçici blob'unu yalnız `AuthenticatePerson`'ın eski kolu üretir. İşletmelerden **çoğul** söz eden üç sabit
  gerekçe metni tekil ikizine çevrilir (`identityReasons`, `adminloginidentity.go:193-206`). Bağlanan eski
  satır sayısı (`PersonSignIn.Linked`) yalnız log satırına gider, audit'e hiç gitmez
  (`adminloginidentity.go:127-128`, `internal/adminauth/identity.go:88-91`). Bayrak kapalıyken detail
  bugünküdür.
- **`adminLoginWorkLimit` = 106.** Sabit artık türetilir (`adminratelimit.go:744-760`): operatör modelinin
  karşılaştırma bütçesi `adminLoginWorkComparisons = 120 * 8 = 960`, kimlik yolunda bir girişin bedeli
  `1 + MaxCandidates = 9` karşılaştırmadır (`MaxCandidates = 8`, `manager.go:146`) → `960 / 9 = 106` giriş /
  10 dk. Bayrak kapalı yol (8 karşılaştırma) da aynı 106 ile sınırlıdır (848 karşılaştırma, eski tavanın
  altında). Contract A eski pencereyi kaldırınca bölen 1'e iner ve sabit yeniden türetilir. Sevk edilen
  değeri `TestPanelConstants_ShippedValuesArePinned` sabitler.
- **Açılıştan önce geçersizleşen giriş → 409.** Doğrulanan kimlik bilgisi oturum yazılırken artık
  tutmuyorsa (`ErrProofStale`, satır yok ya da `40P01` — `SignInLapsed`, `manager.go:870-876`) yanıt 500
  değil `409 "Please sign in again"`'dir (`signInAgain`, `adminloginidentity.go:158-167`; kimlik yolu
  `:78-83`, bayrak kapalı yol `adminlogin.go:1303-1308`). Metin "hiçbir şey değişmedi" demez: kimlik
  yolunda bu durum, açılıp iptal edilmiş kanıt oturumlarından sonra da gelebilir
  (`adminloginidentity.go:39-48`).
- **Açılıştan sonra hata → 500 + oturum iptali.** `identity_open_session` commit ettikten sonraki bir hata
  (ör. yazılamayan bir bağlama) kimlik oturumunu isteğin iptalini yok sayarak (`context.WithoutCancel`)
  iptal eder ve `PersonSignIn.Abandoned` olarak döner (`internal/adminauth/identity.go:276-284`; alan ve tip
  `:92-105`). Handler o işletmeye aynı oturumu adlandıran bir `admin.login.failed` satırı yazar
  (`outcome = session_abandoned`; gerekçe iptalin tutup tutmadığını söyler) ve "nothing was changed"
  demeyen bir 500 döner (`abandonedLogin`, `adminloginidentity.go:132-156`). Bu dal geçersizleşme
  denetiminden **önce** gelir (`:73-77`), yani açılıştan sonraki bir `40P01` da 500'dür.

### 5.7 Birleştirme

**Kural: iki satır yalnız kişi her ikisinin kimlik bilgisini kanıtlarsa birleşir** (K-2). "Aynı
e-posta" kanıt değildir (00011 saldırısı; kayıt herkese açık, doğrulama yok — ADR 0013 `:304-316`).
Kanıt = **iki taze canlı oturum** (`identity_link_legacy`; her ikisinde `authenticated_at` son 5
dakika içinde — R3):
1. **Girişte otomatik** (§5.6) — aynı parola.
2. **Hesap sayfasında** — kimlik oturumuyla eski parola ya da eski oturumla kimlik parolası
   (en çok 8 aday, dolgulu). Mevcut oturum çoğu zaman 5 dakikadan eskidir, bu yüzden form **mevcut
   oturumun parolasını da yeniden ister** ve o istekte **iki tarafta da** taze oturum açılır (S6);
   doğrulanan her satır bağlanır, karşı taraftaki geçici oturum iptal edilir. Sonuç tek tip ("bağlanacak işletme yok" ile "parola yanlış" ayırt edilmez).
3. **Operatör birleştiremez** (K-12; ADR 0020 `:1094-1095`).
4. `unlinked_at` damgalı (kişinin reddettiği) satır hiçbir yoldan bağlanmaz (R1).

### 5.8 Backfill, uzlaştırma, onay penceresi

**Backfill** (M14-06): her dolu e-posta grubu için — tek satır → kimlik + bağ; çok satır → **en
eski aktif** satırdan (yoksa en eskiden) kimlik + yalnız o satır bağlanır (`linked_via='backfill'`),
öbürleri bağlanmamış kalır. E-postasız satıra kimlik yok. Migration başında rolün superuser ya da
BYPASSRLS olduğu doğrulanır (FORCE RLS altında 0 satırı sessizce görürdü); sonunda **e-postalı satır
varsa** bağlı sayı > 0 doğrulanır — boş bir veritabanında (CI, testcontainers, yeni geliştirme
veritabanı; migration'lar seed'den önce koşar) iddia koşulsuz geçer (bulgu 22, R11).

**Uzlaştırma** (M14-06b, idempotent, aynı incumbent-first kuralı, `unlinked_at` damgalı satırları
atlar): backfill'den sonra doğan satırlar için kimlik yaratır/bağlar (`linked_via='reconcile'`).
Expand'den **sonraki** deploy'da ve M14-19'dan **hemen önce** koşar (bulgu 6b, 23). Binary'yi
expand'in altına geri almak yalnız uzlaştırmayla birlikte.

**M14-08a notu (2026-10-26; kullanıcı kararı — §5.1'in adres kuralı):** backfill ve uzlaştırma yalnız
**dolu ve ASCII** e-postalı satırlardan kimlik doğurur (kural tam olarak `mail.ValidRecipient`'tir,
00076'nın deseni). Uzlaştırmada grubun adayları kuralı geçen satırlardır: bir homoglif satırı grubun en
eski aktif satırı olsa bile **seçilmez**, kimlik grubun kuralı geçen satırının yazımı ve digest'iyle
doğar; yalnız kuralı geçmeyen satırlardan oluşan grup kimlik doğurmaz. `00073` uygulanmış bir
migration olduğu için değiştirilmedi (CLAUDE.md §6); onun tarafı Deploy 1'den önceki salt-okuma
sayımıdır (`deploy/README.md`, Expand'in ikinci ön kontrolü: backfill adaylarında yazdırılabilir ASCII
olmayan adres = 0, değilse **dur**); ön kontrol ile `00073` arasındaki pencereyi `00076`'nın Up'ı
aynı goose koşusunda kapatır (yazdırılabilir ASCII olmayan bir kimlik → yalnız sayıyla hata). Kalan
ASCII-dışı satırlar **adlandırılmış kalıntıdır**: hiçbir yoldan bağlanmaz ve kimlik doğurmaz; M14-19'un
"kimliği olmayan dolu e-posta = 0" ön kontrolü onları ayrı sayar, K-3'ün son tarihi ve contract A'nın
ön koşulu karşısında ne olacakları kullanıcının kararıdır. **İkinci adlandırılmış kalıntı** (M14-08a
üçüncü göz denetimi, 2026-10-27): ASCII ama biçimi kurala uymayan adresler (`a..b@x.mt`, `"q"@x.mt` —
eski `deliverableShape`'in geçirdiği şekiller). `00073` bunlara da kimlik doğurur; citext ASCII'de yalnız
harf büyüklüğünü katladığı için bu bir gasp değildir, ama o kimliğe posta gidemez. Ön kontrolün üçüncü
sayımı (`failing_send_rule`, 6b'nin yüklemi) onları sayar; DUR değildir, sayı deploy'dan önce kullanıcıya
bildirilir.

**Onay penceresi** (bulgu 15, R1, R2, S3–S5): iki kapsamla açılır —
- **`first_verify`** — kimliğin **ilk** doğrulanması (doğrulama linki, davet/kayıt tıklaması):
  `linked_via IN ('backfill','reconcile','signup')` üyelikleri listelenir.
- **`reset`** — **her** sıfırlama tüketimi (satır-başı ya da kimlik linki; kimliğin denetimi el
  değiştirdiği an): pencere açılmadan **önce** kurulmuş (`linked_at < confirm_opened_at`) **bütün** bağlar
  listelenir, her `linked_via` dahil (S3) — önceden ekilmiş bir saldırganın, expand ile M14-17 arasında
  kurbanın adresiyle açtığı işletmeleri kendi girişinde `password` ile bağlaması da görünür. **`claim` ve
  `invite` de listelenir** (T1): iletilmiş bir link ile önceden ekilmiş kimlik onları da kurabilir;
  yalnız `first_verify` kapsamı onları dışarıda bırakır (S7). Contract B'den sonra doğmuş bir satır için
  "çöz" = bağ korunur + `unlinked_at` + `status='disabled'`; oturum **veren** yollar `unlinked_at IS NULL` süzer
  (ONAY notu 2, §5.2).
- **Varsayılan "hepsini tut"** (T11): her sıfırlama tam listeyi açtığı için liste tek tıklık "Keep all"
  ile kapanır; çözme bilinçli, satır satır bir eylemdir.
- Satır-başı sıfırlama da `email_verified_at`'i doldurur (linke tıklayan posta kutusunun sahibidir);
  doldurmasaydı çözme hiç mümkün olmazdı (S5).
- Pencere olay anında `now() + 7 gün` açılır. **Bayrak kapalıyken** liste yüzeyi görünmez; **M14-19'un
  migration'ı** (deterministik, testli — T9) bayrak kapalıyken açılmış bütün pencerelerin yalnız
  `confirm_open_until`'ini `now() + 7 gün` yapar; `confirm_opened_at` **korunur** (reset listesinin
  kümesi ona bağlıdır) — yani pencere = `max(olay, bayrak günü) + 7 gün` (S5).
- **Geri alınan satır listede kalır ve yeniden çözülebilir** (M14-18, 2026-10-27 kullanıcı kararı, O1):
  `first_verify` kapsamı `'account'` bağlarını da sayar — bu pencerede `'account'` ancak pencerenin kendi
  geri almasıdır (§5.2 notu).
- **"Keep all" tek tıktır ve canlı oturum yeter** (M14-18, 2026-10-27 kullanıcı kararı; T11). **Kabul
  edilen risk, adıyla:** bir oturumu çalan kişi (canlı çerez) pencereyi kapatıp listeyi gerçek sahibinden
  **gizleyebilir** — üyeliklerin hiçbiri çözülmez, ama kişi onları gözden geçirme fırsatını kaybeder.
  Hafifletenler: her sıfırlama tüketimi **bütün** oturumları iptal eder ve yeni bir `reset` penceresi
  açar (çalınmış çerez o anda ölür, liste yeniden açılır); her basış bir `confirm_kept` olay satırı
  bırakır (`admin_identity_events`, 90 gün). Çözme ve geri alma parolayı yeniden sormaya devam eder.
- Çözme taze oturum + kimlik parolasının yeniden girilmesini ister, pencere açıkken
  `identity_relink_refused` ile geri alınır, son aktif owner için açık uyarı gösterir (S4); satıra
  nöbetçi digest + `unlinked_at` yazar → kurbanın parolası saldırganın işletmesini **açmaz**, sonraki
  giriş satırı yeniden bağlamaz, satır-başı sıfırlama ona link göndermez ve tetikleyicinin ikinci kolu
  digest'in üstüne yazılmasını reddeder (S8). Bu, *"daha önce kaydolmuş saldırgan"* durumunu (ADR 0013
  risk 3) kapatır.

### 5.9 Parola sıfırlama

M14-08'den itibaren **bağlı** bir satırın eski linki kimliği yazar (`identity_consume_row_reset`) —
ADR 0015'in *yetkinin nesnesi* değişikliği bayrağa değil **dual-write'a** bağlıdır. M14-14'ten
itibaren talep e-posta → kimlik → **tek** link; bağlanmamış satırlar contract'a dek satır başı link
alır. Her tüketim onay penceresini açar. Linkler GET ile **tüketilmez** (posta tarayıcılarının ön
yüklemesi): bugünkü sıfırlama deseni — GET token'ı kısa ömürlü bir çereze alıp formu gösterir, tüketim
POST'tadır (`adminreset.go:1415-1425`, `readLink`) — doğrulama, davet ve kayıt linklerinde de aynen
uygulanır (R19). ADR 0022 §3/§4/§9/§10 ve EM-7C'nin "hesap başına tek canlı link" kuralı aynen. Kimlik başına talep
(`identity_open_reset`) EM-7C'yi kimlik düzeyinde korur — canlı link varken hiçbir şey basılmaz, iptal
edilmez, gönderilmez — ve teslim edilemeyen link `identity_withdraw_reset` ile geri çekilir (bugünkü
`Withdraw`).

### 5.10 Kayıt sihirbazı (K-9 a)

Doğrulama tıklamasından önce **kimlik, tenant ve üyelik doğmaz** (bulgu 11). `POST /signup` taslağı
`identity_open_signup_claim` ile `admin_signup_claims`'e yazar (parola digest'i dahil, TTL ile), adrese
link gönderir ve her durumda aynı *"Check your inbox"* ekranını gösterir. Link GET'te tüketilmez (§5.9);
onay POST'tur:
- **adreste kimlik yoksa** → onay sayfası **formda seçilen parolayı yeniden ister** ve talebin
  digest'iyle karşılaştırır (R2: linke tıklayan kişi, parolayı seçen kişi olmalı — saldırgan kurbanın
  henüz kayıtlı olmayan adresiyle kendi parolasını yazıp kurbanın tıklamasını bekleyemez; cihazlar arası
  da çalışır, imzalı çerez bağından farklı olarak). Doğruysa uygulama tenant'ı yaratır (`WithTenant`,
  bugünkü `Provision`), `identity_claim_signup` kimliği (`email_verified_at` dolu) ve owner satırını
  yazar; tek transaction. VAT o anda yazılır — iki bekleyen kayıt aynı VAT'ı taşıyorsa ikincisi bugünkü
  VAT çakışması mesajını alır; süresi dolan talep hiçbir şey ayırmaz;
- **adreste kimlik varsa** → kişi **kendi parolasıyla** girer (canlı oturum) ve yeni işletmeyi sahiplenir;
  `identity.email = claim.email` değilse 0 satır (R18). Tıklama, aynı e-postalı kimliğin canlı
  oturumuyla yapıldığından **posta kutusu kanıtıdır** → `email_verified_at` boşsa doldurulur ve onay
  penceresi açılır (orkestratör kararı, §13 K-9) — var olan müşterilere önce sıfırlama **dayatılmaz**;
- **adreste kimlik var ama aktif üyeliği yoksa** (2026-10-27 kullanıcı kararı, M14-17b, `00080`) → kişi
  giriş yapamaz; onay sayfası **kimliğin parolasını** ister, uygulama onu kimliğin digest'iyle
  karşılaştırır ve `identity_claim_signup`'ı o digest'le çağırır; definer kişinin kilidi altında digest'in
  hâlâ kimliğinki olduğunu ve aktif üyelik bulunmadığını yeniden denetler (§5.2 M14-17b notu). Tıklama yine
  posta kutusu kanıtıdır (`email_verified_at`, `first_verify`). Aktif üyeliği olan kimlik bu yoldan 0
  satırdır — onun kanıtı canlı oturumdur. Kayıt yanıtı, zamanlaması ve mektup seçimi üç durumda aynıdır
  (R18); bilinen adresin tek mektubu iki kanıtı da anlatır.
Oranlar: `signupCreateLimit` (adres başına) + **alıcı başına** üst sınır (EM-7C'nin `(mailbox)` kovası
biçiminde) (bulgu 11, 32). `signInBlocked` kimlik yolunda emekli.

**Teslim notu (2026-10-26 kullanıcı kararı — S2 "Dev/sandbox'ta linki göster"; uygulama M14-17):**
doğrulama linki `TAPPA_RESET_DELIVERY=email` iken posta ile gider — her gönderim **tek kesiciden**
geçer, alıcı başına tavan `mail.RecipientCap` (`scope="signup"`, posta kutusu başına saatte 5 / günde 20;
davet yoluyla aynı tek tavan örneği). Teslim `none` **ve** kurulum sandbox ya da **açıkça** `TAPPA_ENV=dev` ise (boş `TAPPA_ENV`'nin dev'e düşmesi sayılmaz — M14-17 denetimi) link *"Check
your inbox"* ekranında gösterilir (davetin `panel` modu gibi; `config.RegistrationLinksOnScreen`).
Prod'da (ve staging'de) `none` ise sihirbaz bu kurulumun kayıt almadığını söyler (her adımda 503). Onay
sayfası `/admin/signup/confirm`'dadır: var olan kimliğin kanıtı panel oturumudur ve panel çerezinin
yolu `/admin`'dir. Bayrak bu yüzeyi kapılamaz (§8 tablosu).

**Saat kayması notu (2026-10-27, M14-17b bulgusu, orkestratör kararı):** taslağın bitişini uygulama
kendi saatiyle yazar, `identity_open_signup_claim` ise onu **veritabanının** `now() + 24 saat` tavanıyla
karşılaştırır (`00078`). Bitiş tam tavandayken veritabanı saati uygulamanınkinin gerisinde kaldığında
(geliştirme veritabanında 200 ölçümün 200'ünde öyleydi) kayıt aralıklı olarak reddediliyordu — bayrak bu
yüzeyi kapılamadığı için Deploy 1 canlı kullanıcılara taşırdı. Düzeltme migration'sızdır:
`adminauth.SignupClaimTTL = tavan − signupClaimSkewMargin` (5 dakika); mektup kalan süreyi saat olarak
yazdığı için kişi farkı görmez. Testler: `TestSignupClaimTTL_LeavesTheSkewMarginUnderThe00078Ceiling`
(TTL + pay = tavan, tavan 00078'in metni) ve `TestSignupClaims_OpenSurvivesADatabaseClockBehindTheApplications`
(veritabanı saati 2 sn geride → taslak yazılır; pay 0 → kırmızı). Aynı desen başka yerde yok: davetin
72 saati 7 günlük tavanın, sıfırlamanın 1 saati 24 saatlik tavanın çok altında; doğrulama linkinin bitişi
veritabanında hesaplanır.

**Sıralama (bulgu 7):** M14-17'den önce kayıt, var olan ile yeni e-posta için farklı bir iz bırakır
(bağlanmamış satır / kimlik). Bu fark **yalnız bayrağın kapattığı yüzeylerden** görünür (§8 bayrak
tablosu); bayrak M14-17'den önce açılmaz.

### 5.11 Davet

Owner-only (policy motoru; `m7-portal.md:1570-1573`'ün `deny sid=default` tuzağı), tenant-kapsamlı
`admin_invites` (owner listeler ve iptal eder), tek kullanımlık, süreli, hash'li, davetlinin adresine
gider, panelde gösterilmez. Kabul §5.2'deki iki fonksiyonla, POST ile; rızasız üyelik doğmaz; var olan
kimlikle kabulde tıklama posta kutusu kanıtıdır (§5.10'daki gibi; önce sıfırlama dayatılmaz).
**Davet eden owner, adresin bir kimliği olup olmadığını öğrenmez** (R18): davet yanıtı, zamanlaması ve
davet listesindeki durumu iki durumda aynıdır. Holding üyeliği davetin yerine geçmez.
**Reddedilmiş satıra davet** (ONAY notu 3): tenant'ta aynı adresle reddedilmiş bir satır varsa davet
kabulü yeni satır yaratmaz, **o satırı** yeniden bağlar (owner'ın daveti + kimliğin canlı oturumu +
posta kutusu kanıtı; `unlinked_at = NULL`, contract öncesi digest kopyası, contract sonrası
`status='active'`). Adres o işletmede zaten etkin bir üyeliğe aitse owner'a verilen yanıt tek tiptir
("address already in this business") ve reddedilmiş/etkin ya da kimlikli/kimliksiz ayrımını sızdırmaz.
**Davet yüzeyi bayrakla kapılıdır** (R12): bayrak kapalıyken davetli eski koldan girer ve M7-07'nin
"`resolve_admin_by_email` penceresi" tuzağı (`m7-portal.md:1581-1586`) — davetli 8 adaylık pencerenin
dışında kalabilir — geri gelirdi; bayrak açıkken davetli kimlik yolundan girer ve pencere yoktur.

### 5.12 Devre dışı bırakma, sahiplik, operatör

- **Üyelik başına** (K-6): owner o işletmedeki üyeliği `disabled` yapar → yalnız o tenant'taki
  oturumlar düşer. Kimliği kilitleyen bir yol M14'te **yoktur** (bulgu 24).
- **Son owner değişmezi** ve iki adımlı devir (M14-16).
- **Contract ön koşulu** (bulgu 13, R1): *"tek aktif owner'ı bağlanmamış olan tenant sayısı = 0"*
  (runbook, yalnız sayı) — `unlinked_at` damgalı (reddedilmiş) satırların tenant'ları bu sayıya
  **girmez**, kullanıcıya ayrı bir sayı olarak raporlanır (o tenant'lar e-postanın gerçek sahibinin
  reddettiği işletmelerdir). İlk sayı 0 değilse contract durur.
- **Operatör değişmez** (K-12): kimlik tablolarına ve fonksiyonlarına yetki yok. ADR 0025'in ileride
  gelecek `op_disable_tenant_admins`'i (`:620-621`) yalnız üyelik `status`'una dokunabilir, kimliğe
  asla (bulgu 25).

**M14-16 uygulama notları (2026-10-27; kart dışı kararlar, denetime açık).** Migration yok; yeni
`identity_*` fonksiyonu, sütun ya da yetki yok — dört yazma `tappa_app`'ın var olan `UPDATE (role,
status)`'u (`00017:380-381`) ve `RevokeAdminSessionsForAdmin` ile; yeni sorgular `db/queries/adminmembers.sql`
(hiçbiri `identity_id` / bağ sütunlarını anmaz).
- **Dört basış** (owner'ın Managers sayfasında, işletmenin üyelik listesi davetlerin yanında): devre dışı
  bırak, yeniden etkinleştir, owner yap, manager yap. **İki adımlı devir** = (1) yeni kişiyi owner yap —
  işletmenin iki owner'ı olur, eski owner geri alabilir; (2) eski owner kendi üyeliğini manager'a indirir
  ("step down", onay kutusu ister). Her adım kendi audit satırıdır (`admin.membership.promoted`,
  `admin.membership.demoted` `{self: true}`); tek bir basış hem sahipliği verip hem kendininkini almaz.
  Kendini devre dışı bırakma / etkinleştirme reddedilir.
- **Son-owner değişmezi panelin kuralıdır, tetikleyici değildir** (`adminauth.Memberships`): değişiklik
  önce işletmenin bütün aktif owner satırlarını id sırasıyla `FOR NO KEY UPDATE` kilitler, aktörün o
  kümede olduğunu (kemer) ve hedef aktif bir owner'sa kümenin ≥ 2 olduğunu denetler, sonra yazar — tek
  transaction, audit satırı dahil. Neden tetikleyici değil: iki meşru yazar bir işletmeyi aktif owner'sız
  bırakır ve bir tetikleyici ikisini de adlandırılmış istisna olarak taşımak zorunda kalırdı —
  `identity_unlink_backfilled` (kişinin kendi kararı; contract sonrası satırı `disabled` yapar — ONAY
  notu 4, "aynı uyarı, izin ver") ve ADR 0025'in gelecek sert kilidi (`op_disable_tenant_admins`).
  Definer'ın yolu yapı gereği etkilenmez; `TestAdminMembers_TheDefinersUnlinkIsNotStoppedByTheLastOwnerRule`
  hem davranışı (son owner'ın çözülmesi contract öncesi ve sonrası geçer) hem `admin_users`'taki
  tetikleyici kümesini pinler. Eşzamanlılık: `TestMemberships_TwoStepDownsRaceAndOneIsRefused`.
- **Reddedilmiş satır panelden yeniden açılamaz:** `tappa_app` `unlinked_at`'i okuyamadığı için (R16)
  panel reddedilmiş bir satırı `active` yapabilir; bu onu açmaz — oturum veren her yol `unlinked_at IS
  NULL` süzer (`TestAdminMembers_EnablingARefusedRowGivesNoSession`). **Adlandırılmış kalıntı (kullanıcı
  kararı 2026-10-27: M14-16 için kabul, ayrı kart backlog'da):** aynı sebeple değişmez reddedilmiş bir
  owner satırını aktif owner sayar (contract ön koşulunun ayrı saydığı satır). Bunun için bir basış
  **gerekmez**: contract öncesi çözme satırı `active` bırakır, satır panelde zaten "Owner · Active"
  görünür ve sayılır; contract sonrasında bir owner onu etkinleştirince sayılır. Owner böyle bir satırın
  yanında çekilirse işletmede oturum alabilen owner kalmaz ve **ürün içinden geri dönüş yoktur**: davet
  ve owner yapma yalnız owner'ındır ve hiçbiri giriş yapamaz. Geri dönüş yalnız veritabanı ya da operatör
  müdahalesidir.
- **Devre dışı bırakma yalnız o satırın o işletmedeki oturumlarını** iptal eder (`RevokeAdminSessionsForAdmin`,
  `identity_revoke_sessions` değil — K-6; `TestMemberships_DisablingDropsOnlyThisBusinesssSessions`, iki
  işletmeli). Bu iptal, o anda commit edilmekte olan bir oturumu **görmez**: `identity_open_session` /
  `identity_switch_session` satırı kilitsiz okuyup oturumu ekler, devre dışı bırakmadan önce okumuş biri
  devre dışı satıra canlı bir oturum bırakabilir (güvenlik denetimi ölçtü). Satır devre dışıyken o oturum
  kullanılamaz (`TouchAdminSession`); **yeniden etkinleştirme de satırın bütün oturumlarını aynı
  transaction'da iptal eder** — kişi yeniden giriş yapar
  (`TestMemberships_EnablingRevokesASessionThatLandedWhileDisabled`). Kalan pencere: oturum açan
  transaction'ın hem devre dışı bırakmanın hem etkinleştirmenin commit'ini kapsayacak kadar sürmesi.
- **Owner kapısı M14-15'inkidir** (`a.mayInvite` → `tenant.RuleWriter.MayInviteManager`, `policy:edit`,
  materyalize edilmemiş kitapta sevk edilen baseline): yetki vermek / almak işletmenin yetkilendirmesini
  değiştirmektir ve yeni bir eylem materyalize kitaplarda `deny sid=default` alırdı.
- **Askı** (ADR 0025'in M14 notu): daraltan iki basış (devre dışı bırakma, manager yapma) K-26'nın
  gerekçesiyle askıda da açıktır (`protectWritingThroughSuspension`); genişleten ikisi (etkinleştirme,
  owner yapma) askı halkalı ana gruptadır. Dördü de bayrakla kapılı ve tenant bağlıdır.
- Audit satırları yalnız üyelik id'sini (hedef), sabit kelimeleri ve devre dışı bırakmada ya da yeniden
  etkinleştirmede iptal edilen oturum sayısını taşır — adres, ad, kimlik, başka işletme, üyelik sayısı yok.
- **Onay kutuları:** "Step down" ve "Make owner" (bütün işletmeyi tek basışta verir) ikisi de
  `confirm=yes` ister; handler denetler.
- **API anahtarları (§5.13, backlog T174):** bir owner'ı devre dışı bırakan ya da manager yapan her
  basışın yanında, o kişinin oluşturduğu API anahtarlarının çalışmaya devam ettiğini söyleyen cümle ve
  API Keys bölümüne bağlantı durur — T174 karara bağlanana dek.

### 5.13 API anahtarları, webhook'lar, aktörler

Değişmez: `api_keys.created_by` üyeliğin bileşik FK'si (`00051:88-89`); devre dışı bir üyeliğin
anahtarları bugünkü gibi çalışır (ADR 0028 ele almıyor — backlog notu). Webhook'ların aktörü yok
(`00048:186-199`). Aktör id'leri üyelik id'si kalır → bir tenant'ın günlüğü başka tenant'taki üyelik
id'sini görmez.

### 5.14 E-posta şablonları (bulgu 32)

Yeni ya da değişen şablonlar, hepsi ADR 0022 §4 (sabit ASCII konu, alıcı kuralı), §3 (sızıntı), §9
(oranlar) ve EM-7C sınırlarıyla: **yönetici daveti** · **kayıt doğrulaması** (yeni adres) · **var olan
adresle kayıt** ("bu adresle bir işletme kaydı başlatıldı — sizseniz giriş yapıp sahiplenin"; 2026-10-27
kullanıcı kararı, M14-17b: aynı tek mektup aktif üyeliği olmayan kimliğe de gider ve iki kanıtı birlikte
anlatır — aynı tarayıcıda giriş ya da linkin açtığı sayfada parolayı yazmak; R18, mektup üyeliğe göre
seçilmez) ·
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
| Tenant sınırını geçen hareketler | Giriş, geçiş, davet kabulü, kayıt sahiplenme, bağlama, sıfırlama — her biri bir **sırra** anahtarlı (üç adlandırılmış istisna hariç), sonucu tek-tenant bir oturum ya da tek-tenant satır yazmaları. Bir istek asla iki tenant'ta koşmaz — **aynı anda** demektir: art arda iki ayrı `WithTenant` (tenant id'si veritabanından, istekten değil) bu kurala uyar; hesap sayfasının taze oturumlarına yazılan sıradan `admin.logout` satırı bunun örneğidir, girişteki proof oturumları emsalidir (M14-18 güvenlik denetimi, 2026-10-27) |
| BYPASSRLS gövdeleri | Tenant tablosuna dokunan her ifade açık `tenant_id` yüklemi taşır (kuşak + kemer) |
| `tappa_app`'ın kimlik bağı yazması | Sütun düzeyinde kapalı + bağlı satır tetikleyicisi (INVOKER) |
| Çapraz sızıntı | Audit satırları karşı tenant'ı, üyelik sayısını, `identity_id`'yi, `linked_via`'yı anmaz; liste yalnız kişinin kendi oturumuyla |
| Adlandırılmış kalıntı | `tappa_app` `admin_users.identity_id`'yi **okur** (geçiş oran kovası) — tarama testleriyle sınırlı (§5.1) |

### 6.1 M14-01'in CLAUDE.md'ye uygulayacağı metin (birebir)

§4.5'te *"**Bilinçli istisnalar iki tanedir** — tenant sınırını aşmanın yalnız bu iki yolu var:"*
→ *"**Bilinçli istisnalar üç tanedir** — tenant sınırını aşmanın yalnız bu üç yolu var:"*;
*"Bu iki istisnanın dışında kalan dış akış yazıcısı"* → *"Bu üç istisnanın dışında kalan dış akış
yazıcısı"*; (2) maddesinin sonundaki parantezden sonra şu madde eklenir:

> **(3) Panel yöneticisinin kimliği** (ADR 0031) — bir kişi tek kimlikle birden çok işletmeye üye
> olur. Kişi verisi tenant kapsamsız beş tabloda durur (`admin_identities`, `admin_identity_resets`,
> `admin_identity_verifications`, `admin_signup_claims`, `admin_identity_events`); `tappa_app`'ın
> bunlarda hiçbir yetkisi yoktur; `admin_users.identity_id` / `linked_via` / `linked_at` /
> `unlinked_at`'i yazamaz, `linked_via` / `linked_at` / `unlinked_at`'i okuyamaz. Erişim yalnız ADR 0031 §5.2'deki **kapalı** `identity_*`
> listesiyle (sahibi `tappa_identitydefiner`: NOLOGIN, BYPASSRLS, NOSUPERUSER, üyesiz, varsayılan
> yetkisi yok, yalnız sütun düzeyi GRANT) ve salt okuma `resolve_identity_*` / `resolve_admin_invite_*`
> / `resolve_signup_claim_*` çözümleyicileriyle (`tappa_resolver`, ADR 0002 md.7) olur. Kimliği yazan
> her fonksiyon kimliği bir **sırdan** bulur (canlı oturum, sıfırlama/davet/kayıt/doğrulama token'ı);
> adlandırılmış üç istisna `identity_open_session` (Go tipiyle bağlı, 00011 YÜKÜMLÜLÜK 5'in
> karşılığı), geçici `identity_create_for_signup` (kendi transaction'ında yazılmış tek satır; M14-17'den
> sonra kalkar) ve `identity_record_failed_login`'dir (yalnız `admin_identity_events`, kimlik kovasıyla
> sınırlı). Bu kuralın dışında kalan üç bakım yazarının uygulama yolu yoktur: `identity_reconcile()`
> (`EXECUTE` yalnız `tappa_owner`'da; M14-06b'nin migration'ı ve M14-19'dan önce runbook çağırır,
> contract B'de düşer), M14-06'nın backfill migration'ı (`tappa_owner`, ADR 0031 §5.8) ve M14-19'un
> migration'ı (`tappa_owner`; yalnız bayrak kapalıyken açılmış onay pencerelerinin `confirm_open_until`'i,
> ADR 0031 §5.8, §8 adım 5); bunların ve adlandırılmış üç istisnanın dışında kimliği ya da
> `admin_users`'ın bağ sütunlarını (`identity_id`, `linked_via`, `linked_at`, `unlinked_at`) sırsız yazan
> yoktur. Kimlik yazmayan ama sırra da
> anahtarlı olmayan yazarlar adıyla şunlardır:
> `identity_open_signup_claim` (yalnız taslak satırı, oranla sınırlı), `identity_open_reset` (yalnız bir
> sıfırlama linki + bir `reset_requested` olayı; kimlik başına tek canlı link, EM-7C) ve iki sabit periyotlu süpürme. BYPASSRLS gövdelerinde tenant tablosuna dokunan **her** ifade açık bir `tenant_id` yüklemi
> taşır. Bir tenant'a yazan her fonksiyon o tenant'ın `audit_log`'una yazar; satır karşı tenant'ı,
> kişinin üyelik sayısını ve `identity_id`'yi anmaz; bir istek her an tek tenant bağlamında koşar.
> `tappa_app`'ın `admin_users.identity_id`'yi okuması (geçiş oran kovası) adlandırılmış bir kalıntıdır ve
> ADR 0031 §5.1'deki tarama testleriyle sınırlıdır. `tappa_operator` ve `tappa_opdefiner` hiçbir
> `identity_*` / `resolve_identity_*` fonksiyonunu çalıştıramaz ve kimlik tablolarında yetki taşımaz.
> **Listeye fonksiyon eklemek, dönüş sütunlarını, definer'ın sütun matrisini ya da `tappa_app`'ın veya
> `tappa_resolver`'ın kimlik sütunlarındaki ve tablolarındaki yetkilerini genişletmek kullanıcı onayı +
> ADR 0031 değişikliği ister** (testler: ADR 0031 §5.2'deki imza katalog testi ve sütun matrisi testi;
> yetkiler: ADR 0031 §5.1, §5.2).

§6'nın *"**Tenant kapsamsız tablo** (yalnız platform operatörü — ADR 0020/0021 — ve holding
üyeliği: …)"* parantezi şöyle genişler: *"(yalnız platform operatörü — ADR 0020/0021 —, holding
üyeliği: `holdings`, `holding_members`, yazarı yalnız `op_*` — ADR 0027 — ve panel yöneticisinin
kimliği: `admin_identities`, `admin_identity_resets`, `admin_identity_verifications`,
`admin_signup_claims`, `admin_identity_events`, yazarı yalnız `identity_*` — ADR 0031)"*.

§3 dizin haritasına `internal/adminauth` için bir satır eklenir: *"panel yöneticisinin kimliği ve
oturumu (ADR 0031); kimlik tablolarına yalnız `identity_*` / `resolve_identity_*` ile gider"*.

**ADR 0002 md.7 notu** (M14-01, R22): md.7'nin sorgu sayımı (`0002:289-296`, *"sayı garanti değildir,
garanti beş kısıttır"*) bu ADR'nin **beş** yeni salt-okuma çözümleyicisini adıyla sayar —
`resolve_identity_by_email`, `resolve_identity_reset_by_token_hash`, `resolve_admin_invite_by_token_hash`,
`resolve_signup_claim_by_token_hash`, `resolve_identity_verification_by_token_hash` — ve yazan
`identity_*` fonksiyonlarının md.7 değil §4.5 (3) olduğunu söyler.

## 7. `manager.go:285-292` saldırısı ne olur

| Aşama | Durum |
|---|---|
| Bugün | `Verified` + imzalı set + `selectVerified` ile kapalı; yan bedeller: DoS (`MaxCandidates`), sayım ve zamanlama kanalları, kalabalıklaştırma |
| Expand, bayrak kapalı (P5 dahil) | **Görünür davranış** aynı: bağlı ve bağlanmamış oturumun görebileceği her farklı yüzey bayrakla kapalı (§8 tablosu). **İçeride değişen:** kayıt bilinmeyen adrese kimlik yaratır, bilinen adreste satırı bağlanmamış bırakır (`identity_create_for_signup`); bağlı satırın parolası/sıfırlaması kimliği ve bütün bağlı satırları yazar (dual-write). Bu fark dışarıdan gözlenemez çünkü onu gösterecek yüzeyler kapalı (R4) |
| Bayrak açık (yalnız M14-17'den **sonra**) | E-posta tek kimliğe çözülür; saldırganın ektiği satır bağlanmamış eski satırdır, eski kol bugünkü korumalarıyla sürer; kayıt doğrulamasız kimlik doğurmaz |
| Onay penceresi | İlk doğrulamada ve her sıfırlamada backfill / uzlaştırma / kayıtla bağlanmış üyelikler kişiye onaylatılır; çözülen satır nöbetçi digest + kalıcı ret alır (§5.8) |
| Contract | Eski kol kalkar; `admin_users.email` ve `password_hash` doğrulamada okunmaz → sınıf **yapısal olarak** kapanır; `MaxCandidates`, dolgu, `PickerCap`, `signInBlocked`, ADR 0013'ün ön-kayıt sınırı emekli |

`manager.go:285-292` yorumu contract'ta silinmez, tarihsel olarak işaretlenir ve bu ADR'ye bağlanır.

## 8. Geçiş — expand → bayrak → contract (iki deploy)

**Bayrak** `TAPPA_ADMIN_IDENTITY` (`off` | `on`): `deploy/k8s/05-config.yaml`'da `off` olarak doğar,
`internal/config` onu doğrular, paketleme testleri (ör. `TestPackaging_NoConfigMapKeyIsAlsoAnEnvEntry`)
güncellenir. Dosya her deploy'da uygulanır (`deploy.yml:808`) ve pod env'i başlangıçta okur
(`20-app.yaml:282-284`): **bayrağı çevirmek = `05-config.yaml` commit'i + `main` deploy'u = kullanıcı
kararı.** `kubectl edit` ile yapılan bir geri dönüş bir sonraki deploy'da sessizce geri çevrilir;
runbook bunu yasaklar (bulgu 5).

**Bayrağın kapattığı yüzeyler** (R4, seçenek (a): bağlı ve bağlanmamış bir oturumun farklı
görebileceği her yüzey kapılıdır). Bayrak `off` iken:

| Yüzey | `off` | `on` |
|---|---|---|
| Giriş | eski yol (bugünkü), herkes için | kimlik yolu + eski kol |
| `GET/POST /admin/switch` | 404 | açık |
| Kabukta "Switch business" bağlantısı | yok | var |
| Kabukta işletme adı (§5.3) | var — her oturumda aynı, bağdan bağımsız | var |
| Hesap: "Your businesses", "Verify your e-mail", onay listesi, "Link a business", "Sign out everywhere" | yok (sayfa bugünkü gibi) | var |
| Yönetici daveti gönderme ve kabul (M14-15) | yok (404) | var |
| Üyelik yönetimi (M14-16) | yok | var |
| Kimlik başına sıfırlama talebi (M14-14) | eski satır-başı talep | kimlik başına |
| Kayıt doğrulaması (M14-17) | **açık** — iki adres için de aynı "Check your inbox" ekranı, gözlenebilir fark yok | açık |
| Tenant bağlı formlar (M14-10a) | açık (tek tenant'lı oturumda uyuşmazlık imkânsız) | açık |
| Dual-write, bağlı satır tetikleyicisi | açık | açık |

Kabul (M14-08, M14-19): bayrak `off` iken bağlı tek-üyelikli bir oturum ile bağlanmamış bir oturum
kabuk, hesap sayfası ve `/admin/switch` için **bayt-aynı** yanıt alır (işletme adı ve oturuma özgü
token'lar dışında).

1. **Rol** canlı kümeye (runbook, `tappa_owner`) — migration'dan önce.
2. **Expand deploy**: eklemeli migration'lar (tablolar, sütunlar, tetikleyici, fonksiyonlar,
   backfill) + yeni binary, bayrak `off`, dual-write açık. **Eski binary yeni şemada** çalışır (kayıt
   INSERT'i `identity_id NULL` ile; bağlı satır parola yazması tetikleyiciyle 500 — adlandırılmış bedel).
3. **Uzlaştırma deploy'u** (M14-06b) — pencerede doğan satırlar.
4. **P5 kartları** canlıda (bayrak hâlâ `off`; yüzeyleri kapalı, M14-17 açık).
5. **M14-19 bayrak `on`**: ön kontroller — kimliği olmayan dolu e-posta = 0 (`unlinked_at` damgalılar
   hariç); `count(identity_id IS NOT NULL AND password_hash <> kimliğin digest'i) = 0`; `count(identity_id
   IS NOT NULL AND unlinked_at IS NOT NULL) = 0` (S19); uzlaştırma yeniden koşturuldu; bayrak kapalıyken
   açılmış onay pencereleri bayrak gününe taşınır. Aynı deploy'un migration'ı (T9) iki şey yapar:
   `identity_create_for_signup`'tan `tappa_app`'ın `EXECUTE`'unu alır — M14-17'den sonra hiç çağrılmaz;
   DROP contract B'de kalır (S18) — ve açık pencerelerin yalnız `confirm_open_until`'ini `now() + 7 gün`
   yapar (S5). Geri dönüş = `off` commit'i + deploy.
6. **Soak** — K-3'ün 60 günü = `max(bayrağın açıldığı gün, M14-14 ve M14-18'in canlıya çıktığı gün)`;
   bu sırada M14-14 ve M14-18 bayraktan önce canlıda olduğundan saat **bayrağın açıldığı gün** başlar —
   kullanıcının kabul ettiği metin (R5, §13).
7. **Contract A** (M14-20a): binary eski kolu çağırmayı bırakır (`identity_create_for_signup`'ı
   zaten M14-17 bırakmıştır), bayrak kaldırılır; **şema değişmez** → bu deploy'un rollout penceresindeki eski pod hâlâ çalışır.
8. **Contract B** (M14-20b, **sonraki** deploy), sırayla (R6, R8, R10):
   1. Bağlı satır tetikleyicisi **düşürülür ve yerine** contract sonrası kuralı konur: bağlı satırda
      `password_hash` yalnız `NULL` olabilir, `email` yalnız `tappa_identitydefiner` tarafından değişir;
      reddedilmiş (`unlinked_at`) satır kolu aynen kalır. (Eski tetikleyici "herkes NULL yazabilir" diye
      **gevşetilmez**; değiştirilir.) "Bağlı her satırda digest eşitliği" değişmezi bu adımda **açıkça
      emekli** olur.
   1b. **Digest kopyalayan definer gövdeleri aynı imzalarla `CREATE OR REPLACE` edilir** (S1):
      `identity_set_password`, `identity_consume_reset`, `identity_consume_row_reset`,
      `identity_link_legacy`, `identity_relink_refused`, `identity_accept_invite`,
      `identity_accept_invite_new`, `identity_claim_signup` artık satırlara digest **kopyalamaz**; yeni
      bağlı satırlar `password_hash NULL` doğar. **`identity_unlink_backfilled`** de listededir (T3):
      contract'tan sonra eski kol yoktur, nöbetçi digest gereksizdir → çözülen satıra `NULL` yazar; yeni
      tetikleyici kuralı "bağlı satırda yalnız NULL" bu yüzden çözme ile çatışmaz. İmzalar aynı olduğundan contract A binary'si çalışmaya
      devam eder; imza katalog testi değişmez, gövde testleri "bağlı satırda digest NULL"a döner.
   2. Bayat digest kopyaları `NULL` (`tappa_owner`, yeni tetikleyiciyle uyumlu); `password_hash`
      NULL'lanabilir.
   3. `DROP FUNCTION resolve_admin_by_email`, `DROP FUNCTION identity_create_for_signup` ve `DROP FUNCTION
      identity_reconcile` (T3).
   4. `tappa_app`'tan `INSERT (email, password_hash)` ve `UPDATE (password_hash)` `REVOKE`.
   5. `CHECK (identity_id IS NOT NULL OR created_at < '<contract B anı>')` — **VALID** (`created_at`
      uygulamaya yazılamaz, `00017:379-416`); `NOT VALID` bir CHECK sonraki UPDATE'leri de denetlerdi
      ve contract'tan sonra bağlanmamış eski bir satırı devre dışı bırakmak (M14-16) onu ihlal ederdi.
   Rollout penceresindeki eski pod (contract A binary'si) düşen hiçbir şeyi çağırmaz (bulgu 4).

**Down'lar:** expand Down'ı yalnız eklediğini kaldırır (backfill Down'ı `linked_via='backfill'`).
Contract B'nin Down'ı kimlik digest'ini satırlara geri kopyalayabilir ve eski tetikleyiciyi geri
koyar; geri alınamayan şey **silinen koddur**, ve contract'tan sonra davetle doğmuş `password_hash NULL`
satırlar `NOT NULL` geri gelmeden önce doldurulmalıdır (bulgu 28).

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
- **Çözme (R1):** çözülen satırın digest'i kimliğinkinden farklıdır ve hiçbir parolayla eşleşmez; aynı
  parolayla sonraki giriş satırı bağlamaz ve satır eski kolda kimseyi içeri almaz; pencere kapalıyken
  çözme 0 satır.
- **Tıklayanı bağlama (R2):** kayıt onayı yanlış parolayla bir şey yaratmaz; doğrulama token'ı başka bir
  oturumla (başka tarayıcı) tıklanınca hiçbir şeyi doğrulamaz; her sıfırlama tüketimi onay penceresini
  açar.
- **Tazelik (R3, S4, S6):** `authenticated_at` 5 dakikadan eski ya da `NULL` olan oturumla bağlama ve
  çözme 0 satır; 6 dakikalık bir oturum parolayı yeniden girince hesap sayfasından bağlama başarılı.
- **Ret (S4, S8):** çözme pencere açıkken geri alınır, sonra alınamaz; reddedilmiş satıra satır-başı
  sıfırlama linki gitmez ve `tappa_identitydefiner` dışında hiçbir rol onun digest'ini değiştiremez;
  bayrak geri alınınca eski giriş yolu reddedilmiş satırı açmaz.
- **Önceden ekilmiş kimlik (S3):** ekilmiş kimlik + `password` bağı + kurbanın sıfırlaması → bağ
  listelenir ve çözülebilir.
- **Eski kol penceresi (T2):** 8 bağlı + 1 bağlanmamış satırı olan kişi doğru parolayla girince
  bağlanmamış satır otomatik bağlanır.
- **İletilmiş link (T1):** ekilmiş kimlik + iletilmiş linkle `claim` + kurbanın sıfırlaması → `claim` bağı
  `reset` listesinde görünür; contract sonrası doğmuş satırda "çöz" üyeliği devre dışı bırakır.
- **Geri alma (T4):** yalnız bu pencerede reddedilmiş satır geri bağlanır; arada davetle doğmuş üyelik
  varsa 0; geri bağlanan satır pencere sonuna dek listede kalır.
- **Kayıt sahiplenme (S2):** var olan bir tenant'ın id'siyle `identity_claim_signup` → 0 satır;
  sahiplenmeden sonra doğan `claim` satırı listede yok (S7).
- **Contract B (S1):** contract B şemasında contract A binary'siyle parola değiştir, sıfırla, davet
  kabul et, kaydol, bağla → hepsi yeşil, bağlı satırlarda digest `NULL`.
- **Eski sekme (R7):** bağ sorgu parametresinden okunur; büyük gövdeli bir POST, bağ uyuşmazsa
  handler'ın sınırından önce ayrıştırılmadan reddedilir.
- **Mutasyon kanıtları:** geçişte kimlik eşitliği · `status='active'` · eski oturum iptali · sütun
  GRANT'ı · tetikleyici (ve DEFINER'a çevrilmesi) · e-postayla çözülen dual-write · bağlamada ikinci
  oturum şartı · bağlamada tazelik yüklemi · çözmede nöbetçi digest · doğrulamada oturum bağı · audit'e
  hedef tenant · oran kovası oturum id'sine dönünce → her biri kırmızı.
- **Eşzamanlılık:** N paralel geçiş → bir kazanan, kaybedenler `Set-Cookie` yazmaz (`-race`); aynı
  e-postayla eşzamanlı iki kayıt → 500 yok, bir kimlik (R13).
- **Zamanlama:** beş giriş durumu düz (§5.6).
- **Bayrak (R4):** bayrak `off` iken bağlı ve bağlanmamış oturumun kabuk, hesap ve `/admin/switch`
  yanıtları bayt-aynı.

## 10. Sonuçlar ve kabul edilen riskler

**Kazanılan:** istenen deneyim; giriş çözümleyicisi md.7'nin ≤ 1'ine döner; DoS, sayım, zamanlama ve
kalabalıklaştırma kanalları emekli; parola/sıfırlama/"her yerden çık" tek yerde; M7-07 doğal evinde.

**Ödenen:** üçüncü §4.5 istisnası (beş tenant-kapsamsız tablo, bir definer rolü, kapalı liste); iki
giriş kolu (contract'a dek); iki contract deploy'u; kayıt akışına e-posta doğrulaması.

**Kabul edilen riskler:**
- **`signInBlocked` erken emekli (2026-10-27, M14-17):** contract yerine M14-17'de emekli oldu (kayıt artık kimlik yoluyla ve posta kutusu kanıtıyla biter; bir adres en çok bir kimliğe çözülür). Bedeli: bayrak kapalıyken, tek adreste 8 ya da daha çok satırı olan bir kişi yeni işletmeye eski kol üzerinden ulaşamayabilir (o kol karşılaştırma penceresini hâlâ okur).
- **Çalınan çerezin etki alanı büyür** (bulgu 14): bugün bir panel çerezi tek işletmeye açılır;
  M14'te kişinin bütün işletmelerine geçebilir. Geçiş `authenticated_at`'i taşır (yenilemez), ama
  oturumun sunucu tarafı mutlak ömrü yoktur (`cookie.go:57-75`) — backlog maddesi (mutlak ömür +
  hassas işlemlerde yeniden doğrulama).
- **Expand penceresinde** eski pod'un bağlı satıra parola yazması 500 alır (§5.1).
- **Üç adlandırılmış istisna sırra anahtarlı değildir** (`identity_open_session`,
  `identity_create_for_signup`, `identity_record_failed_login`) — güvenceleri Go tipinde, yapısal
  yüklemlerde ve kimlik kovasındadır (§5.2).
- `tappa_app` `identity_id`'yi okur (oran kovası) — §4.5'in adlandırılmış kalıntısı, tarama testleriyle
  sınırlı.
- Oturum HMAC anahtarının rotasyonu açık her panel formunu 409'a düşürür (§5.5).
- Bayrağı P5'ten sonra açmak **"çıkmadan geçiş"in canlıya çıkışını geciktirir** (kullanıcıya plan
  özetinde söylenir — R23).

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
çevirdi**. İkinci denetim turunun (R1–R23) iki orkestratör netleştirmesi (K-3, K-7/K-9) kabul edilen
metne geri döner; yeni kullanıcı sorusu yoktur. Aşağıda her karar, seçilen seçenek ve denetim düzeltmelerinin anlamına etkisi.

| # | Karar | Seçilen | Düzeltmenin etkisi |
|---|---|---|---|
| K-1 | E-posta global tekil (kimlik başına) | (a) | — |
| K-2 | Birleştirme yalnız iki kimlik bilgisinin kanıtıyla | (a) | **Anlam sıkılaştı** (bulgu 9): kanıt artık veritabanında **iki canlı oturum**; e-posta eşitliği yalnız savunma katmanı |
| K-3 | Bağlanmamış satırlar için son tarih, 60 gün (bayrak açıldıktan sonra) | (a) | **Anlam değişmedi** (bulgu 20 + R5, orkestratör kararı): saat = `max(bayrağın açıldığı gün, M14-14 ve M14-18'in canlıya çıktığı gün)`; yeni sırada ikisi bayraktan önce canlıda olduğundan saat **bayrağın açıldığı gün** başlar — kullanıcının kabul ettiği metin. Ek contract ön koşulu: tek aktif owner'ı bağlanmamış tenant = 0, reddedilmiş (`unlinked_at`) satırların tenant'ları ayrı raporlanır (bulgu 13, R1) |
| K-4 | Girişte son kullanılan işletme, seçici yok | (a) | **Anlam daraldı** (bulgu 10): "seçici yok" kimlik yoluna aittir; eski kolda birden çok satır doğrularsa seçici contract'a dek kalır |
| K-5 | Kabukta ad + "Switch business" bağlantısı, sayfada liste | (a) | — |
| K-6 | Devre dışı bırakma yalnız üyelik başına | (a) | **Anlam değişti** (bulgu 24): "kimlik kilidi yalnız sistemin" ifadesi kalktı — M14'te kimlik kilidi **hiç yok** (`status` sütunu yok) |
| K-7 | Var olan kimliği yalnız owner, davetle; davetli kendi parolasıyla kabul eder | (a) | **Sıkılaştı** (bulgu 12): davet e-postası kimliğin e-postasına eşit olmalı; kabul oturum vermez; davet eden owner adresin kimliği olup olmadığını öğrenmez (R18); davet yüzeyi bayrakla kapılı (R12). **Orkestratör netleştirmesi (round 2):** var olan müşterilere önce sıfırlama **dayatılmaz** — davet linkine aynı e-postalı kimliğin canlı oturumuyla tıklamak (POST) posta kutusu kanıtıdır, `email_verified_at`'i doldurur ve onay penceresini açar; "kendi parolasıyla" kabul aynen |
| K-8 | Rol işletme başına | (a) | — |
| K-9 | Kayıt e-posta doğrulamasıyla biter; var olan adres aynı ekranı görür | (a) | **Genişledi** (bulgu 7, 11, R2): tıklamadan önce hiçbir şey doğmaz; yeni adreste onay formda seçilen parolayı yeniden ister (tıklayan = parolayı seçen); M14-17 bayrağı açmanın ön koşulu. **Orkestratör netleştirmesi:** var olan adreste kayıt linkine aynı e-postalı kimliğin canlı oturumuyla tıklamak posta kutusu kanıtıdır (K-7 ile aynı) |
| K-10 | Askıdaki işletme değiştiricide işaretli, geçiş serbest | (a) | — |
| K-11 | Sahiplik devri ve kaldırma M14'te | (a) | — |
| K-12 | Operatör değişmez | (a) | **Genişledi** (bulgu 25): gelecekteki `op_disable_tenant_admins` yalnız üyelik durumuna dokunur |
| K-13 | §4.5 sınıflandırması | **(b) — üçüncü açık istisna** | **Değişti (2026-10-21, denetimden sonra).** Gerekçe: güvenlik denetimi, yazan ve tenant sınırını geçen fonksiyonları "md.7 çözümleyicisi" saymanın md.7'nin "salt okuma, bağlam üreten" tanımını genişlettiğini ve sınırı görünmez kıldığını gösterdi; kullanıcı istisnanın **adıyla** yazılmasını seçti. md.7'de yalnız salt okuma çözümleyiciler kalır; yazanlar §6.1'deki kapalı listedir |
| K-14 | `admin_users.email` ayna, `full_name` üyelikte | (a) | **Sıkılaştı** (bulgu 21): `tappa_app`'ın `UPDATE (email)`'i expand'de kapanır |
| K-15 | **Yeni (bulgu 12):** e-posta değiştirme | **(a) — M14'ün dışında** (kullanıcı, 2026-10-22) | Var olan kimliğin adresini değiştirmek mevcut parola + yeni adresin doğrulanması + eski adrese bildirim + çakışmada aynı "gelen kutunu kontrol et" + aynanın bütün işletmelerde definer ile güncellenmesini ister; kendi kartı ve kararıyla gelir. Kullanıcıya M14-01'de soruldu, (a)'yı seçti (2026-10-22) |
