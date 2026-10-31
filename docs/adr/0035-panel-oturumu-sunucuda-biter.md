# ADR 0035 — Panel oturumu sunucuda 12 saatte biter: parola kanıtından mutlak ömür

- **Durum:** Kabul edildi (2026-10-31)
- **Karar veren:** kullanıcı (2026-10-31: *"Sunucuda 12 saat mutlak"* — çerezle aynı an, OWASP'ın mutlak
  oturum zaman aşımı); orkestratör (aynı gün: geçiş zinciri kapanır — saat parola kanıtıdır, geçişin
  çerezi kalan süreyi alır; `tappa_resolver`'a `admin_sessions SELECT (authenticated_at)` kabul edildi);
  nerede denetlendiği ve `NULL`'ın okunuşu T183'ün yapıcı kararı (aşağıda ölçülerek gerekçeli),
  güvenlik denetiminden geçer
- **İlgili:** ADR 0002 md.7 (çözümleyici, "taşı, süzme"), ADR 0031 §5.1 (`authenticated_at`, *"`NULL` =
  eski / süresi geçmiş … ileride gelecek mutlak ömür bunu fail-closed okur"*), §5.2 (kapalı `identity_*`
  listesi, matris), §5.3 (istek başına yol), §5.4 (geçiş), CLAUDE.md §4.5 (3),
  `internal/adminauth/cookie.go`, migration `00085`, backlog T183

## Bağlam

Panel çerezinin `Max-Age`'i 12 saatti (`cookie.go`), ama bu bir **tarayıcı ipucuydu**. `admin_sessions`
`created_at`, `last_used_at`, `revoked_at` ve (M14) `authenticated_at` taşır; `resolve_admin_session_by_token_hash`
(00011) de `TouchAdminSession` da hiçbirini karşılaştırmıyordu. Saklanan ya da kopyalanan bir çerez — curl,
kazınmış bir değer, diskten kopyalanmış bir tarayıcı profili — biri iptal edene dek geçerliydi. Canlıda
2026-10-31'de bir işletmede 48 iptal edilmemiş oturum vardı.

Çerez yalnız yeni bir oturum satırı doğduğunda yazılır: girişte (`adminlogin.go`, `Issue`;
`adminloginidentity.go`, `identity_open_session`) ve işletme değiştirmede (`adminswitch.go`;
`identity_switch_session` yeni satır doğurur, eskisini iptal eder ve eskisinin `authenticated_at`'ini
kilit altında **kopyalar** — 00081).

## Karar

1. **Kural.** Bir panel oturumu yalnız `authenticated_at > now() - 12 saat` iken canlıdır. Saat **parola
   kanıtıdır**, satırın doğumu değil: ilk taslak `created_at` kullanıyordu ve geçiş yeni satır doğurduğu
   için saati yeniden başlatıyordu — iki işletmeli bir kimlikte çerezi tutan biri (meşru kullanıcı da, çalmış
   biri de) on bir saatte bir geçiş yaparak oturumu süresiz uzatabilirdi. Geçiş kanıtı kopyaladığından artık
   saati yeniden başlatmaz; ömür **mutlak**tır (kullanım uzatmaz). Go'daki tek kopya
   `internal/db.AdminSessionLifetime`; çerezin `Max-Age`'i ona testle eşitlenir
   (`TestAdminAuthConstants_ShippedValuesArePinned`), SQL'deki `interval '12 hours'` her yolda saniye
   sınırında ona pinlenir (`internal/db/adminsessionlifetime_test.go`).

2. **Çerez sunucuyla aynı anda biter.** Giriş kanıtı satırla birlikte doğar: çerez tam ömrü alır. Geçişin
   çerezi **kalan süreyi** alır: `adminauth.Manager.Switch`, geçişten hemen sonra yeni işletmenin bağlamında
   `GetAdminSessionSecondsLeft`'i (veritabanının saatiyle, yukarı yuvarlanmış tam saniye) okur ve
   `Issued.Left`'e koyar; `Cookies.SetIssued` bunu `Max-Age` yapar (en az 1 — `net/http` `Max-Age=0`'ı
   *hiç* `Max-Age` olarak yazar, yani sonu olmayan bir çerez; en çok tam ömür). Okuma 0 satır verirse (geçiş
   ile okuma arasında son saniye doldu) geçiş sıradan bir ret olur: çerez yazılmaz, tarayıcıdaki iptal
   edilmiş çerez giriş sayfasına gönderir. `Issued.Left`'in sıfır değeri tam ömürdür (zararsız yön: en kötü
   ihtimalle sunucunun zaten reddettiği bir çerez).

3. **`NULL` bitmiştir** (ADR 0031 §5.1'in kendi cümlesi). Orkestratör `COALESCE(authenticated_at,
   created_at)` yazmıştı, öncülü "`NULL` yalnız expand öncesi satırlarda, hepsi 12 saatten eski". Ölçüldü:
   öncül **yarım** doğru — geçiş `NULL`'ı da kopyalar (`TestIdentitySwitch_ANewSessionTheOldRevokedTheProofCarried`'in
   *"no proof"* durumu 00085'e dek bunu ölçüyordu), yani `NULL` kanıtlı **taze** satırlar doğabiliyordu ve
   `COALESCE` onların saatini her geçişte `created_at`'ten yeniden başlatırdı: kapatılan zincir `NULL`
   üzerinden açık kalırdı. Ayrıca kuşak taraması (`identitybelt_test.go`) bir WHERE bağlacında fonksiyon
   çağrısı kabul etmez. Bu yüzden `NULL` kanıt = bitmiş oturum. Ölçülen: 00085'ten sonra her oturum
   INSERT'i `NULL` olmayan bir kanıt yazar — `CreateAdminSession` (`tappa_app`'ın INSERT'i sütunu
   adlandıramaz; 00069'un `DEFAULT now()`), `identity_open_session` (`now()`), `identity_switch_session`
   (kopya; kaynağı aramada `authenticated_at > sınır`'dan geçtiği için `NULL` değil). `NULL` satırlar
   yalnız 00069'dan (Deploy 1, 2026-10-28) önce doğanlar — deploy anında hepsi 12 saatten eski — ve o
   zamandan beri onlardan geçilenlerdir; ikinci tür deploy'da biter (sahibi yeniden girer). Deploy'un salt
   okuma kontrolü ikisini de sayar (`deploy/README.md`, T183).

4. **Denetim veritabanında, oturum hash'inin ulaştığı her yolda** — bir Go çağıranı unutsa da atlanamasın
   diye; ve yalnız çözümleyicide değil, çünkü güvenlik denetiminin bulgusu tam buydu: hash'i doğrudan alan
   `identity_*` definer'ları çözümleyiciden geçmez.
   - **`resolve_admin_session_by_token_hash` (md.7):** 00011'in ilkesi korunur — **taşı, süzme**. Dönen
     `revoked_at` oturumun **sonu**dur: iptal varsa iptal anı; kanıt yoksa `now()`; ömür dolduysa
     `authenticated_at + 12 saat`. İmza ve dört sütun aynı; `authenticated_at` okunur, döndürülmez (md.7'nin
     beş kısıtı aynen). Böylece iptal edilmiş oturumu reddeden her çağıran bitmişi de reddeder — **00085'ten
     önceki binary dahil** (`adminauth.Verify`'ın `rs.RevokedAt != nil`'i); migration rollout'tan önce
     koştuğu için bu, expand-only bir değişikliktir.
   - **`TouchAdminSession`** (istek başına otorite) aynı yüklemi taşır; **`ListAdminSessionsForAdmin`**
     `revoked_at`'i aynı kuralla döndürür (kanıtsız satırın sonu doğumu) — bitmiş oturum hiçbir listede
     canlı görünmez; **`GetAdminSessionSecondsLeft`** (yeni, yalnız geçişin çerezi için) bitmişte 0 satır.
   - **Oturumu hash'iyle bulan on iki `identity_*` definer'ı** — `identity_memberships`,
     `identity_switch_session`, `identity_revoke_sessions`, `identity_password_for_session`,
     `identity_set_password`, `identity_request_verification`, `identity_verify_email`,
     `identity_accept_invite`, `identity_claim_signup`, `identity_confirm_window`, `identity_keep_all`,
     `identity_link_by_email`: her `WHERE s.token_hash = p_*session_hash`'in ardına
     `AND s.authenticated_at > v_proven_after`; sınır bir `DECLARE` değişkenidir
     (`v_proven_after timestamptz := pg_catalog.now() - interval '12 hours'`) — 00072'nin `v_fresh`'i
     emsal. Kilit altındaki yeniden okumalara (id ile) dokunulmaz. İmzalar aynı (`CREATE OR REPLACE`;
     `SECURITY DEFINER`, `search_path`, sahip, `REVOKE … FROM PUBLIC`, `GRANT EXECUTE … TO tappa_app`
     yeniden yazılır — ADR 0031 §5.2 T5). Kapalı listeye fonksiyon **eklenmez**, CLAUDE.md §4.5 metni
     **değişmez**.
   - **Yeniden tanımlanmayan üç oturum anahtarlı definer**, çünkü aynı sütunda **daha sıkı** bir sınır zaten
     var: `identity_unlink_backfilled`, `identity_relink_refused` ve `identity_link_legacy` (iki oturumu da)
     bir oturumu yalnız `authenticated_at > now() - 5 dakika` iken kabul eder (R3/S4; `NULL` ret) — on iki
     saatin içinde beş dakika. Oraya yazılacak daha gevşek bir yüklem hiçbir şeye karar vermez, hiçbir testle
     kanıtlanamaz; yazılmadı. `TestAdminSessionLifetime_EverySessionLookupInADefinerIsBounded` her hash
     aramasının ya ömür yüklemini ya bu tazeliği taşıdığını pinler (yeni bir definer ikisi de olmadan
     kırmızı).
   - **Dokunulmayanlar, bilerek:** `identity_open_session` ve `identity_accept_invite_new` oturum **doğurur**;
     `identity_consume_reset` / `identity_consume_row_reset` yalnız iptal eder; iptal yazarları
     (`RevokeAdminSession*`, `identity_revoke_sessions`'ın döngüsü) bitmiş satırı da damgalar — zararsız
     (tetikleyici damgayı monotonik tutar) ve 00085'ten önce de yaptıkları buydu.

5. **Yetki — tek değişiklik:** `admin_sessions` `SELECT (authenticated_at)` → `tappa_resolver`
   (orkestratörün kabulü, 2026-10-31). Var olan bir sütun, döndürülmez; `tappa_identitydefiner` onu 00071'den
   beri okur (matris değişmez); `tappa_resolver`'ın kimlik tablolarındaki yetkisi değişmez (`admin_sessions`
   kimlik tablosu değildir). `tappa_app` sütunu hâlâ yazamaz.

6. **Sıcak tabloda şema değişikliği yok:** sütun, indeks, veri yazımı yok; son **hesaplanır** (`revoked_at`
   toplu damgalanmaz, satır silinmez). Sütun GRANT'ı `admin_sessions`'ta kilit almaz (ölçüldü: `pg_locks`
   boş); `CREATE OR REPLACE FUNCTION` da. Down bulduğu on üç gövdeyi bayt bayt geri verir
   (`TestAdminSessionLifetime00085_DownGivesBackWhatItFoundAndUpHEAD`).

## Değerlendirilen seçenekler

- **Satırın doğumu (`created_at`)** — ilk taslak; geçiş zincirini süresiz bırakır. Reddedildi (orkestratör).
- **`COALESCE(authenticated_at, created_at)`** — `NULL` kopyalanan satırlarda zinciri açık bırakır, kuşak
  taramasına sığmaz (Karar 3). Reddedildi.
- **`expires_at` sütunu.** Sıcak tabloya `ALTER` + geri doldurma ister; `authenticated_at + sabit` aynı
  bilgiyi yazımsız verir. Reddedildi.
- **Yalnız Go'da (`Verify`) denetlemek** ya da **çözümleyicide süzmek (0 satır).** Birincisi unutulur ve
  definer'lar Go'nun cevabını görmez; ikincisi 00011'in "taşı, süzme" ilkesini bozar. Reddedildi.
- **Geçişte çerezi tam 12 saat bırakmak.** Tarayıcı sunucudan sonra biterdi (sunucu reddeder, kullanıcı giriş
  sayfasına düşer — zararsız ama "aynı an" kararına aykırı). Reddedildi; kalan süre yazılır.
- **Boşta kalma (idle) zaman aşımı.** Kullanıcı mutlak ömrü seçti; kapsam dışı.

## Sonuçlar ve kabul edilen riskler

- **Deploy anında** kanıtı 12 saatten eski (ya da kanıtsız) her iptal edilmemiş oturum biter; sahibi yeniden
  giriş yapar. Normal bir tarayıcı bu çerezleri zaten düşürmüştü — biten, saklanan ya da kopyalanan çerez ve
  girişinden 12 saat sonrasına uzanmış bir geçiş oturumudur.
- **Kullanıcıya görünen tek fark:** girişten 11 saat sonra işletme değiştiren biri, girişin 12. saatinde (yeni
  işletmede bir saat sonra) yeniden girer — eskiden 23. saate kadar sürerdi. Kararın amacı tam budur.
- **Saat:** gövdeler `now()` (transaction başlangıcı) okur; geçişin kalan süresi veritabanının saatiyle
  okunur, Go'nunkiyle değil — iki uç saat kaymasıyla ayrışmaz.
- Testler: `TestAdminSessionLifetime_TheResolverReportsTheEnd`, `TestAdminSessionLifetime_TouchRefusesAnEndedSession`,
  `TestAdminSessionLifetime_TheListShowsAnEndedSessionEnded`, `TestAdminSessionLifetime_ASwitchChainEndsAtTheSignIn`
  (girişten 12 saat − 1 sn sonra geçiş: yeni oturumun 1 saniyesi kalır, bir saniye sonra çözümleyici,
  Touch ve `identity_memberships` reddeder; kontrol: yüklem `created_at`'e dönünce zincir açılır),
  `TestAdminSessionLifetime_EverySessionLookupInADefinerIsBounded`, `TestAdminSessionLifetime_EveryDefinerRefusesAnEndedSession`,
  `TestVerify_EndsTheSessionAtItsLifetime` ve `TestSwitch_TheNewSessionEndsAtTheSignInsEnd` (`internal/adminauth`,
  uçtan uca), `TestCookies_SetIssuedGivesASwitchedSessionTheTimeLeft`,
  `TestAdminSwitch_TheCookieLivesWhatTheSessionHasLeft` (`internal/handler`).
