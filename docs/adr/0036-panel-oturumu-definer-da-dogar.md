# ADR 0036 — Tek satırlık panel oturumu definer'da doğar: parola kanıtı sorgu metninden veritabanına

- **Durum:** Kabul edildi (2026-11-01; kalan risk ve audit satırı 2026-11-02) — expand (00086, canlı: Deploy 6, 2026-11-02); contract (00087, Karar 3) yazıldı 2026-11-02, canlıya çıktığı deploy'la tamamlanır
- **Karar veren:** kullanıcı (2026-11-01, AskUserQuestion: *"Evet, taşı"* — `tappa_app`'tan
  `admin_sessions` INSERT'i alınır, oturum doğumu kapalı listeye eklenen tek bir SECURITY DEFINER
  fonksiyona taşınır; aynı gün *"§4.5'e ekle, devam"* — fonksiyon CLAUDE.md §4.5 (3)'ün sayımına girer,
  aşağıdaki kalan risk kabul edildi); adı, imzası ve gövdesi T187'nin yapıcı kararı, güvenlik
  denetiminden geçer
- **İlgili:** ADR 0031 §5.1 (`authenticated_at`, R15), §5.2 (kapalı `identity_*` listesi, matris, T5),
  §5.6 (giriş, düz zamanlama), §8 (expand → contract); ADR 0035 (12 saatlik ömür); 00011 YÜKÜMLÜLÜK 5;
  CLAUDE.md §4.5 (3); migration `00086`, `00087`; backlog T187

## Bağlam — ölçülen

- `tappa_app`'ın `admin_sessions` yetkisi (`\dp`, goose 85): `INSERT (id, tenant_id, admin_user_id,
  token_hash)` (`00071:574-575`), tablo düzeyi `SELECT`, `UPDATE (last_used_at, revoked_at)`
  (`00011:436-437`). `authenticated_at`'i adlandıramaz, `DEFAULT now()` alır (00069).
- Parola kanıtı `CreateAdminSession`'ın **sorgu metnindedir** (`db/queries/admins.sql`):
  `a.status = 'active' AND a.password_hash = @verified_digest ... FOR NO KEY UPDATE OF a`. Veritabanı bunu
  zorunlu kılmaz: `tappa_app` olarak gönderilen başka bir `INSERT INTO admin_sessions (tenant_id,
  admin_user_id, token_hash) VALUES (...)` devre dışı bir yöneticiye, reddedilmiş bir üyeliğe
  (`unlinked_at` — `tappa_app` sütunu okuyamaz, R16) ya da hiç digest'siz taze, 12 saatlik bir oturum
  doğurur; ADR 0035'in ömrü bunu sınırlamaz.
- Bu binary'de `tappa_app` olarak `admin_sessions`'a yazan üretim yolları **iki**, ikisi de
  `CreateAdminSession`: `adminauth.Manager.Issue` (`internal/adminauth/manager.go`, giriş; tek çağıranı
  `internal/handler/adminlogin.go`'daki giriş) ve `openLegacyProofSession` (`internal/adminauth/identity.go`,
  bağlama kanıtının eski satır tarafı). Başka hiçbir üretim ifadesi (`db/queries`, elle yazılmış SQL,
  seed, script) `admin_sessions`'a INSERT etmez.
- Definer'da doğan oturumlar: `identity_open_session` ve `identity_switch_session`
  (`tappa_identitydefiner`). ADR 0035 Karar 4'ün "`identity_accept_invite_new` oturum doğurur" cümlesi
  ölçülen gövdeye uymuyordu: gövde `admin_sessions`'ı anmaz (`pg_proc.prosrc`; ADR 0031 §5.2 de "oturum
  vermez" der) — ADR 0035'te tek satırla düzeltildi.

## Karar

1. **Fonksiyon** (kapalı listeye eklenir, ADR 0031 §5.2'nin imza tablosu biçiminde):

   | Fonksiyon | Anahtar | Ne yapar / ne zaman reddeder |
   |---|---|---|
   | `identity_open_row_session(p_admin_user_id uuid, p_tenant_id uuid, p_token_hash text, p_verified_digest text, p_purpose text)` → `TABLE (session_id uuid, tenant_id uuid, admin_user_id uuid)` | ⚠️ **sır değil — yazar ama kimlik yazmaz**; Go tipiyle bağlı | Tek bir `admin_users` satırında panel oturumu doğurur, `authenticated_at = now()`. Satırın `FOR NO KEY UPDATE` kilidi altında satır `(id, tenant)` **ve** `status = 'active'` **ve** `unlinked_at IS NULL` **ve** `password_hash = p_verified_digest` değilse, ya da `p_tenant_id` transaction'ın `app.tenant_id`'si değilse (NULL dahil), ya da `p_purpose` `'sign_in'` ya da `'link_proof'` değilse (NULL dahil), ya da bir argüman NULL ise 0 satır. Oturumun tenant'ı ve yöneticisi kilitli satırınkidir. Her doğumda o işletmenin `audit_log`'una **tam bir** satır yazar (Karar 4). `last_login_at` yazmaz (girişin `MarkAdminLoggedIn`'i yerinde kalır) |

   - **Sahip `tappa_identitydefiner`.** Gerekçe okuduklarıdır: `admin_users.unlinked_at` (`tappa_app`
     okuyamaz — R16; asıl kazanç budur) ve `password_hash`, `status`; `admin_sessions`'a
     `authenticated_at` ile yazmak. Bunların hepsi definer'ın matrisinde zaten vardır (00071: `admin_users
     SELECT (id, tenant_id, status, unlinked_at)`, `UPDATE (last_login_at)` — satır kilidi;
     00074: `admin_users SELECT (password_hash)`; 00071: `admin_sessions INSERT (tenant_id, admin_user_id,
     token_hash, authenticated_at)`, `SELECT (id)`). **Yeni GRANT yok, matris değişmez.** `tappa_resolver`
     yazamaz (md.7); yeni bir rol `01-roles.sql` + runbook ister ve hiçbir şey kazandırmaz.
   - `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, `REVOKE ALL … FROM PUBLIC`,
     `GRANT EXECUTE … TO tappa_app` (T5). BYPASSRLS gövdesindeki her tenant tablosu ifadesi açık `tenant_id`
     yüklemi taşır: okuma `a.tenant_id = p_tenant_id`, yazma kilitli satırdan gelen `v_tenant` (kuşak
     taraması). Kimlik tablosu okumaz ve yazmaz. Token yalnız hash olarak gelir, döndürülmez, hiçbir
     hataya ve log'a girmez.
   - **Bağlı/bağlanmamış satır (00069, ADR 0031 §5.6):** fonksiyon `identity_id`'ye bakmaz. Bayrak `off`
     iken eski giriş herkes içindir ve bağlı satırın digest'i kimliğinkinin kopyasıdır (contract B'ye dek);
     bayrak `on` iken eski kol yalnız bağlanmamış, `unlinked_at IS NULL` satırları getirir. Reddedilmiş
     üyelik (`unlinked_at` dolu) her durumda reddedilir — eski ifade bunu yalnız ret işleminin nöbetçi
     digest'i üzerinden görebiliyordu, fonksiyon açıkça söyler.

2. **Go.** `internal/db.OpenRowSession(ctx, tx, admin, tenant, sessionHash, verified PasswordHash, purpose RowSessionPurpose)`, çağıranın
   `WithTenant` transaction'ında (`ClaimSignup` / `SignInOwners` emsali). sqlc kullanılmadı: sqlc v1.28
   `RETURNS TABLE` fonksiyonlarını tipleyemez (`db/queries/resolve.sql`'in ölçümü; repo'daki bütün
   definer çağrıları elle yazılmıştır). İki çağıran, her biri kendi amacıyla: `Issue` →
   `db.RowSessionSignIn` (Proof satırın güncel digest'ine karşı tuttuktan sonra, aynı transaction'da
   `MarkAdminLoggedIn`) ve `openLegacyProofSession` → `db.RowSessionLinkProof`
   (`TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone` ikisini ve amaçlarını pinler). `pgx.ErrNoRows`
   tek rettir; `SignInLapsed` onu "yeniden giriş yap" okur (değişmedi). Yanıt `Session.CreatedAt`'i
   taşımaz — `identity_open_session` yolundaki gibi; üretimde okuyan yok.

3. **Expand → contract (ADR 0031 §8).**
   - **Expand (00086, bu değişiklik):** fonksiyon eklenir, yeni binary onu çağırır. **Hiçbir şey REVOKE
     edilmez:** 00086'dan önceki binary rollout penceresinde `CreateAdminSession`'ın INSERT'ini gönderir ve
     çalışmaya devam eder (migrate Job rollout'tan önce koşar; replicas 1, maxSurge 1, maxUnavailable 0).
     Sorgu `db/queries/admins.sql`'de kalır (eski binary'nin ifadesinin bu şemada hâlâ açtığını kanıtlayan
     testler onun üretilmiş metnini gönderir); üretim kodundan çağrılmaz.
   - **Contract — adlandırılmış takip (T187'nin ikinci yarısı), SONRAKİ deploy'da**, ön koşulu canlıda
     00086'lı binary'nin rollout'unun tamamlanmış olmasıdır (rollout penceresindeki eski pod artık 00086'lı
     binary'dir ve INSERT göndermez). **✅ Yazıldı 2026-11-02 — `00087_revoke_direct_admin_session_inserts.sql`**
     (ön koşul ölçüldü: Deploy 6 = main 232f06f8, goose 86 canlıda; o binary'nin iki doğumu `db.OpenRowSession`'ı
     çağırır, `CreateAdminSession`'ın üretim çağıranı yoktur — `TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone`
     o ağaçta yeşil; migrate Job rollout'tan önce koşarken hizmet veren eski pod definer'dan doğurur). Dört adım:
     1. migration: `SET LOCAL lock_timeout`; `REVOKE INSERT ON admin_sessions FROM tappa_app` (tablo düzeyi
        REVOKE sütun yetkilerini de alır — ölçüldü: sonra `has_column_privilege` sekiz sütunun sekizinde `f`; REVOKE
        tabloda kilit almaz, açık bir ROW EXCLUSIVE tutucusunun yanında anında döner — 00085'in GRANT ölçümü); Up'ın
        son ifadesi bir son koşul (`has_any_column_privilege` `t` ise hata); Down `GRANT INSERT (id, tenant_id,
        admin_user_id, token_hash)` — 00071'inki bayt bayt;
     2. `CreateAdminSession` `db/queries`'ten silinir, `make gen`;
     3. `tappa_app` olarak `admin_sessions`'a INSERT eden test fikstürleri sahip rolüne taşınır; RLS
        testlerinin `tappa_app` INSERT vakaları 42501 beklentisine döner; 00086'nın expand testi (aşağıda)
        "INSERT yok, eski ifade 42501" olur;
     4. `rowsession_test.go`'nun `internal/store` muafiyeti kalkar.

     Yapılan (00087 ile aynı commit): `CreateAdminSession` silindi, `make gen`; `tappa_app` olarak oturum ekleyen
     fikstürler sahip rolüne (`internal/db`'nin RLS fikstürü, bileşik FK testi) ya da doğumun kendisi test
     ediliyorsa definer'a (`OpenRowSession`: `newAdminSession`, `newSessionRow`, devre dışı / yabancı / sıfır
     yönetici retleri) taşındı; RLS yazma testinin `admin_sessions` vakası ve 00011'in sahtecilik vakası 42501
     "permission denied" bekler; 00071'in testi "INSERT yok, eski ifade 42501" olur; 00086'nın testi INSERT'i
     pinlemez, contract'ın kendi testi `TestRevokeAdminSessionInsert00087_NoAppInsertAndTheDownGivesBack00071s`
     (aşağıda). `CreateAdminSession`'ın kendi M14-07 testlerinden ikisi (digest değişimi, açık ret) silindi —
     aynı iki koşulu 00086'nın testleri fonksiyon üzerinde ölçer; üçüncüsü (`tappa_app`'ın satır kilidi FORCE
     RLS altında onundur) `TestAdminUsersRowLock_IsTappaAppsUnderForceRLS` adıyla kalır, çünkü üyelik
     değişikliklerinin sahip kilidi (`db/queries/adminmembers.sql`) hâlâ `tappa_app`'ındır.
   - Down (00086): fonksiyonu düşürür, başka hiçbir şeye dokunmaz.

4. **Audit satırı definer'da** (güvenlik denetimi ORTA, 2026-11-02; kullanıcı kararı *"Denetim kaydı ekle,
   devam"*; amaç argümanı orkestratör kararı (A), aynı gün). CLAUDE.md §4.5 (3): bir tenant'a yazan her
   fonksiyon o tenant'ın `audit_log`'una yazar — kardeşleri `identity_open_session` ve
   `identity_switch_session` giriş satırını definer içinde yazar. Her doğum **tam bir** satır yazar,
   amaca göre; satırlarda token, digest, e-posta, kimlik ya da başka işletme yoktur:

   | `p_purpose` | Çağıran | Satır |
   |---|---|---|
   | `'sign_in'` | `Manager.Issue` | `admin.login.succeeded`, aktör ve hedef yönetici, detay `{outcome: "ok", session_id}` — `identity_open_session`'ın ifadesi, handler'ın `adminLoginSucceededEvent`'iyle bayt bayt aynı |
   | `'link_proof'` | `openLegacyProofSession` | yeni eylem `admin.session.link_proof`, aktör ve hedef yönetici, detay yalnız `{session_id}` |

   - **Sessiz kol yok:** başka her değer (NULL dahil) kilitten önce 0 satırdır; saldırgan iz bırakmayan bir
     amaç seçemez. Panelde audit eylemleri için kapalı bir sözlük/etiket haritası yoktur (ölçüldü: eylemler
     yerel `IN (…)` süzgeçleriyle okunur, `admin.membership.linked` gibi kimlik eylemleri yalnız SQL'de
     yaşar); yeni eylem bir yere kaydedilmez.
   - **ADR 0031 §5.6 korunur:** bağlama akışında eski satırın işletmesine *"`admin.membership.linked`
     girer, giriş satırı girmez"* — kanıt oturumu giriş değildir, `admin.session.link_proof` yazar
     (`TestAuthenticatePerson_TheSamePasswordLinksAProvenLegacyRow` değişmedi).
   - **Çift satır yok:** `completeLogin` (internal/handler) artık kendi giriş satırını yazmaz —
     `completePersonLogin`'in kimlik yolundaki deseni. `TestPanelE2E_SignInAndOut` gerçek veritabanında
     tam bir satır sayar; `TestIdentityAudit_TheRowSessionsRowIsTheOrdinaryOne` satırı handler'ın satırıyla
     sütun sütun karşılaştırır. 00086'dan önceki binary definer'ı hiç çağırmaz: kendi INSERT'inden sonra
     handler'ı tek satırı yazar, veritabanı hiç yazmaz
     (`TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction`) — rollout penceresinde de bir
     satır.
   - **Yeni GRANT yok:** `audit_log INSERT (tenant_id, actor_id, action, target, detail)` definer'ın 00071
     hücresidir. `audit_log`'dan `tappa_app`'ın DELETE'i yoktur (00006) — Tehdit 1'e karşı iz kalır.
   - Oturum çerezi yazılamazsa (`completeLogin`'in ulaşılamaz dalı) iz artık başarı satırı + `session_cookie_failed`
     satırıdır — kimlik yolunun `completePersonLogin`'iyle aynı.

## Kabul edilen kalan risk (kullanıcı, 2026-11-01; düzeltilmiş tabloyla yeniden kabul 2026-11-02)

İlk metin "kanıtın dışındaki her koşul veritabanına taşındı" diyordu; bu, iki denetimin (2026-11-02) ölçtüğü
gibi **abartıydı**. Doğrusu iki ayrı tehdit için ayrı ayrı:

- **Tehdit 1 — `tappa_app` olarak keyfî SQL koşturan bir süreç.** Bu saldırgana karşı `status`, tenant ve
  digest koşulları **engel değildir**: `tappa_app` `admin_users`'ta `UPDATE (status)` ve `UPDATE
  (password_hash)` taşır (00017), `app.tenant_id`'yi kendisi yazar (`set_config(…, true)`) ve digest'i
  okur (`resolve_admin_by_email`, `GetAdminPasswordHashByID`). Yani satırı yeniden `active` yapar,
  istediği tenant bağlamını kurar, digest'i okur ve fonksiyon ona **taze, 12 saatlik bir oturum doğurur.**
  Veritabanının bu saldırgana karşı gerçekten zorunlu kıldığı yalnız şunlardır. **Dört madde de contract'tan
  sonra geçerlidir (00087, 2026-11-02; canlıda 00087'nin deploy'uyla):** expand penceresinde (00086 ile 00087
  arası) `tappa_app`'ın doğrudan INSERT'i hepsini atlıyordu (ölçüldü: o yolda audit satırı yok). 00087'den beri
  `tappa_app` `admin_sessions`'a hiçbir sütunla INSERT edemez (42501;
  `TestRevokeAdminSessionInsert00087_NoAppInsertAndTheDownGivesBack00071s`), dolayısıyla dört madde her oturum
  doğumu için geçerlidir:
  - reddedilmiş üyelik (`unlinked_at`) oturum almaz — `tappa_app` sütunu ne okuyabilir ne yazabilir (R16);
  - oturumun id'si ve kanıt anı (`authenticated_at = now()`) seçilemez;
  - her doğum işletmenin `audit_log`'una `tappa_app`'ın silemeyeceği bir satır bırakır (00006 DELETE'i
    alır; Karar 4);
  - oturum doğuran ikinci bir ifade kalmaz (`CreateAdminSession` 00087'de silindi).
- **Tehdit 2 — binary'nin kendi hatası** (yanlış bir `(admin, tenant)` çifti, atlanmış bir koşul, bayat bir
  kanıt, unutulmuş bir sorgu yüklemi). `status`, tenant bağlamı, `tenant_id` yüklemi ve güncel digest
  koşulları **buna karşı** korur: bunlar önceden yalnız sorgu metnine emanetti, artık veritabanındadır.

Kullanıcı bu düzeltilmiş tabloyu 2026-11-02'de *"Denetim kaydı ekle, devam"* kararıyla yeniden kabul etti.
Parolayı veritabanında doğrulamak (pgcrypto `crypt`) reddedildi: seçici yolunda `Verified` imzalı çerezle
taşınır ve `Issue` anında parola yoktur; düz metin parola bir sorgu parametresi olur; bcrypt maliyeti ikiye
katlanır ve §5.6'nın düz zamanlaması değişir. Tehdit 1'in tek tam çaresi süreç bütünlüğüdür — ele
geçirilmiş bir uygulama süreci girişte yazılan parolaları zaten görür.
- `identity_open_session` (istisna 1) ile aynı güven: tek çağrı yolu Go'daki doğrulanmış tiptir
  (`Verified` / `verifiedIdentity`), fonksiyon değil.

## CLAUDE.md §4.5 (3) (kullanıcı kararı, 2026-11-01: *"§4.5'e ekle, devam"*)

§4.5 (3) kapalı listenin sırra anahtarlı olmayan, kimlik yazmayan yazarlarını **tek tek sayar**;
`identity_open_row_session` tam bu sınıftadır. Sayıma tek bir ad + parantez eklendi — *"(yalnız tek
satırlık panel oturumu; satırın kilidi altında güncel digest + aktif + reddedilmemiş üyelik, Go tipiyle
bağlı — ADR 0036)"* — aynı commit'te ADR 0031 §6.1'in birebir metnine
(`TestClaudeMD_TheIdentityExceptionIsADR0031sSection61`), §5.2'nin "⚠️ işaretlilerin hepsi bunlardır"
listesine ve §5.2'nin imza tablosuna bir satır olarak. §4.5'in başka hiçbir cümlesi değişmedi.

## Testler

- `TestIdentityOpenRowSession_BirthsTheSessionWithItsProofNow`, `TestIdentityOpenRowSession_RefusesEveryRowItMustNot`,
  `TestIdentityOpenRowSession_WaitsForAnOpenUnlinkAndThenRefuses`,
  `TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction` (`internal/db/adminrowsession_test.go`);
  `TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone` (`internal/adminauth/rowsession_test.go`);
  `TestIdentityAudit_TheRowSessionsRowIsTheOrdinaryOne` ve `TestPanelE2E_SignInAndOut` (`internal/handler`,
  audit satırının biçimi ve sign-in başına tam bir satır).
- `TestRevokeAdminSessionInsert00087_NoAppInsertAndTheDownGivesBack00071s` (`internal/db/adminrowsession_test.go`,
  contract, 00087): `tappa_app`'ın `admin_sessions`'ta INSERT'i yok (sekiz sütunun sekizi, tablo düzeyi de),
  SELECT'i ve `UPDATE (last_used_at, revoked_at)`'i aynen; 00086'dan önceki binary'nin ifadesi 42501; definer'dan
  doğum çalışır (pozitif kontrol); Down 00071'in sütun yetkisini bayt bayt geri verir ve eski ifade yeniden açar,
  Up yine alır. `TestAdminSessions00071_TheApplicationCannotChooseAuthenticatedAt` "INSERT yok" bekler.
- **Contract hatırlatıcısı** (güvenlik denetimi DÜŞÜK, 2026-11-02; 00087 ile kapandı — test artık INSERT'i
  pinlemez, yalnız 00086'nın Down/Up'ını ölçer): `TestIdentityOpenRowSession00086_ExpandsAndItsDownDropsOnlyTheFunction`
  `tappa_app`'ın INSERT'ini pinlerdi ve mesajı contract'ın kalan adımlarını adıyla sayardı. "00086'dan büyük bir
  migration varken INSERT duruyorsa kırmızı" biçimi seçilmedi: bir sonraki ilgisiz migration'ı (00087 başka
  bir şeridin) kırmızıya çevirir, oturumlarla ilgisi olmayan işi buna bağlardı; contract'ın plandaki yeri
  `docs/plan/state.md`'dir.
- Katalog: ADR 0031 §5.2'nin imza tablosu (`identitycatalog_test.go`, `identitySignaturesT187`), "her GRANT
  kullanılır" egzersizi, kuşak taraması, `privcatalog_test.go`, `m11schema_test.go`, ömür sınıflandırması
  (`adminsessionlifetime_test.go`: "births").
