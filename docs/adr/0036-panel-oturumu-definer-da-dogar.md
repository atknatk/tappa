# ADR 0036 — Tek satırlık panel oturumu definer'da doğar: parola kanıtı sorgu metninden veritabanına

- **Durum:** Kabul edildi (2026-11-01) — expand (00086); contract sonraki deploy'da (Karar 3)
- **Karar veren:** kullanıcı (2026-11-01, AskUserQuestion: *"Evet, taşı"* — `tappa_app`'tan
  `admin_sessions` INSERT'i alınır, oturum doğumu kapalı listeye eklenen tek bir SECURITY DEFINER
  fonksiyona taşınır; aynı gün *"§4.5'e ekle, devam"* — fonksiyon CLAUDE.md §4.5 (3)'ün sayımına girer,
  aşağıdaki kalan risk kabul edildi); adı, imzası ve gövdesi T187'nin yapıcı kararı, güvenlik
  denetiminden geçer
- **İlgili:** ADR 0031 §5.1 (`authenticated_at`, R15), §5.2 (kapalı `identity_*` listesi, matris, T5),
  §5.6 (giriş, düz zamanlama), §8 (expand → contract); ADR 0035 (12 saatlik ömür); 00011 YÜKÜMLÜLÜK 5;
  CLAUDE.md §4.5 (3); migration `00086`; backlog T187

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
   | `identity_open_row_session(p_admin_user_id uuid, p_tenant_id uuid, p_token_hash text, p_verified_digest text)` → `TABLE (session_id uuid, tenant_id uuid, admin_user_id uuid)` | ⚠️ **sır değil — yazar ama kimlik yazmaz**; Go tipiyle bağlı | Tek bir `admin_users` satırında panel oturumu doğurur, `authenticated_at = now()`. Satırın `FOR NO KEY UPDATE` kilidi altında satır `(id, tenant)` **ve** `status = 'active'` **ve** `unlinked_at IS NULL` **ve** `password_hash = p_verified_digest` değilse, ya da `p_tenant_id` transaction'ın `app.tenant_id`'si değilse (NULL dahil), ya da bir argüman NULL ise 0 satır. Oturumun tenant'ı ve yöneticisi kilitli satırınkidir. Audit satırı ve `last_login_at` yazmaz (girişin `MarkAdminLoggedIn`'i ve handler'ın giriş satırı yerinde kalır; bağlama kanıtı bir giriş değildir) |

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

2. **Go.** `internal/db.OpenRowSession(ctx, tx, admin, tenant, sessionHash, verified PasswordHash)`, çağıranın
   `WithTenant` transaction'ında (`ClaimSignup` / `SignInOwners` emsali). sqlc kullanılmadı: sqlc v1.28
   `RETURNS TABLE` fonksiyonlarını tipleyemez (`db/queries/resolve.sql`'in ölçümü; repo'daki bütün
   definer çağrıları elle yazılmıştır). İki çağıran: `Issue` (Proof satırın güncel digest'ine karşı
   tuttuktan sonra, aynı transaction'da `MarkAdminLoggedIn`) ve `openLegacyProofSession`. `pgx.ErrNoRows`
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
     binary'dir ve INSERT göndermez):
     1. migration: `SET LOCAL lock_timeout`; `REVOKE INSERT ON admin_sessions FROM tappa_app` (tablo düzeyi
        REVOKE sütun yetkilerini de alır); Down `GRANT INSERT (id, tenant_id, admin_user_id, token_hash)`;
     2. `CreateAdminSession` `db/queries`'ten silinir, `make gen`;
     3. `tappa_app` olarak `admin_sessions`'a INSERT eden test fikstürleri sahip rolüne taşınır; RLS
        testlerinin `tappa_app` INSERT vakaları 42501 beklentisine döner; 00086'nın expand testi (aşağıda)
        "INSERT yok, eski ifade 42501" olur;
     4. `rowsession_test.go`'nun `internal/store` muafiyeti kalkar.
   - Down (00086): fonksiyonu düşürür, başka hiçbir şeye dokunmaz.

## Kabul edilen kalan risk (kullanıcı, 2026-11-01)

- **Kanıt güncel digest'tir, parola değil — ve `tappa_app` digest'i okuyabilir.** `resolve_admin_by_email`
  ve `GetAdminPasswordHashByID` digest'i `tappa_app`'a döndürür; `tappa_app` `password_hash`'i yazabilir de
  (00017). Dolayısıyla `tappa_app` olarak **keyfî SQL koşturabilen bir süreç bir satırın digest'ini okuyup
  bu fonksiyona vererek hâlâ taze, 12 saatlik bir panel oturumu basabilir.** T187'nin cümlesi bu yüzden
  tamamen kapanmaz; kullanıcı bunu 2026-11-01'de *"§4.5'e ekle, devam"* kararıyla kabul etti.
- **Veritabanının artık zorunlu kıldığı (önceden yalnız sorgu metninde olan):** devre dışı (`status`) ya
  da reddedilmiş (`unlinked_at` — `tappa_app`'ın okuyamadığı sütun) üyeliğe oturum yok; satırın kilit
  altındaki **güncel** digest'i olmadan oturum yok (araya giren parola değişimi, sıfırlama ya da ret 0
  satır); başka tenant'ta ya da transaction'ın tenant bağlamı dışında oturum yok; oturumun tenant'ı ve
  yöneticisi kilitli satırınkidir; id ve kanıt anı (`authenticated_at = now()`) seçilemez. Contract'tan
  sonra `tappa_app`'ın oturum doğuran ikinci bir ifadesi kalmaz. Parolayı veritabanında doğrulamak
  (pgcrypto `crypt`) reddedildi: seçici yolunda `Verified` imzalı çerezle taşınır ve `Issue` anında parola
  yoktur; düz metin parola bir sorgu parametresi olur; bcrypt maliyeti ikiye katlanır ve §5.6'nın düz
  zamanlaması değişir. Bu sınıfın tek tam çaresi süreç bütünlüğüdür — ele geçirilmiş bir uygulama süreci
  girişte yazılan parolaları zaten görür.
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
  `TestOpenRowSession_HasItsTwoCallersAndTheOldInsertNone` (`internal/adminauth/rowsession_test.go`).
- Katalog: ADR 0031 §5.2'nin imza tablosu (`identitycatalog_test.go`, `identitySignaturesT187`), "her GRANT
  kullanılır" egzersizi, kuşak taraması, `privcatalog_test.go`, `m11schema_test.go`, ömür sınıflandırması
  (`adminsessionlifetime_test.go`: "births").
