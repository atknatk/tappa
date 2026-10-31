# ADR 0035 — Panel oturumu sunucuda 12 saatte biter: satırın doğumundan mutlak ömür

- **Durum:** Kabul edildi (2026-10-31)
- **Karar veren:** kullanıcı (2026-10-31: *"Sunucuda 12 saat mutlak"* — çerezle aynı an, OWASP'ın mutlak
  oturum zaman aşımı); nerede denetlendiği, yetki hücreleri ve testler T183'ün yapıcı kararı (aşağıda
  gerekçeli), güvenlik denetiminden geçer
- **İlgili:** ADR 0002 md.7 (çözümleyici, "taşı, süzme"), ADR 0031 §5.1 (`authenticated_at`), §5.2 (kapalı
  `identity_*` listesi, fiil × sütun matrisi), §5.3 (istek başına yol), §5.4 (geçiş), CLAUDE.md §4.5 (3),
  `internal/adminauth/cookie.go`, migration `00085`, backlog T183

## Bağlam

Panel çerezinin `Max-Age`'i 12 saatti (`cookie.go`), ama bu bir **tarayıcı ipucuydu**. `admin_sessions`
`created_at`, `last_used_at`, `revoked_at` ve (M14) `authenticated_at` taşır; `resolve_admin_session_by_token_hash`
(00011) de `TouchAdminSession` da hiçbirini karşılaştırmıyordu. Saklanan ya da kopyalanan bir çerez — curl,
kazınmış bir değer, diskten kopyalanmış bir tarayıcı profili — biri iptal edene dek geçerliydi. Canlıda
2026-10-31'de bir işletmede 48 iptal edilmemiş oturum vardı.

Çerez yalnız yeni bir oturum satırı doğduğunda yazılır: girişte (`adminlogin.go`, `Issue`;
`adminloginidentity.go`, `identity_open_session`) ve işletme değiştirmede (`adminswitch.go`;
`identity_switch_session` yeni satır doğurur, eskisini iptal eder). Yani normal bir tarayıcıda oturum zaten
satırın doğumundan 12 saat sonra ölüyordu; sunucu bunu bilmiyordu.

## Karar

1. **Kural.** Bir panel oturumu yalnız `created_at > now() - 12 saat` iken canlıdır. Ömür **mutlak**tır
   (kullanım uzatmaz) ve satırın doğumundan sayılır; geçiş yeni satır doğurduğu için yeni bir ömür başlatır —
   çerezle aynı an. Go'daki tek kopya `internal/db.AdminSessionLifetime`; çerezin `Max-Age`'i ona testle
   eşitlenir (`TestAdminAuthConstants_ShippedValuesArePinned`), SQL'deki `interval '12 hours'` her yolda
   saniye sınırında ona pinlenir (`internal/db/adminsessionlifetime_test.go`).

2. **Denetim veritabanında, oturum hash'inin ulaştığı her yolda** — bir Go çağıranı unutsa da atlanamasın
   diye; ve yalnız çözümleyicide değil, çünkü güvenlik denetiminin bulgusu tam buydu: hash'i doğrudan alan
   `identity_*` definer'ları çözümleyiciden geçmez.
   - **`resolve_admin_session_by_token_hash` (md.7):** 00011'in ilkesi korunur — **taşı, süzme**. Dönen
     `revoked_at` oturumun **sonu**dur: iptal varsa iptal anı, yoksa ömür dolduğunda `created_at + 12 saat`.
     İmza ve dört sütun aynı; `created_at` okunur, döndürülmez (md.7'nin beş kısıtı aynen). Böylece iptal
     edilmiş oturumu reddeden her çağıran süresi dolmuşu da reddeder — **00085'ten önceki binary dahil**
     (`adminauth.Verify`'ın `rs.RevokedAt != nil`'i); migration rollout'tan önce koştuğu için bu,
     expand-only bir değişikliktir.
   - **`TouchAdminSession`** (istek başına otorite) aynı yüklemi taşır: çözümleyicinin cevabına bakmayan bir
     çağıran yine 0 satır alır.
   - **Oturumu hash'iyle bulan on beş `identity_*` definer'ı** (on altı arama; `identity_link_legacy`'de iki):
     `identity_memberships`, `identity_switch_session`, `identity_revoke_sessions`,
     `identity_password_for_session`, `identity_set_password`, `identity_link_legacy` (kimlik oturumu **ve**
     eski oturum), `identity_unlink_backfilled`, `identity_relink_refused`, `identity_request_verification`,
     `identity_verify_email`, `identity_accept_invite`, `identity_claim_signup`, `identity_confirm_window`,
     `identity_keep_all`, `identity_link_by_email`. Her `WHERE s.token_hash = p_*session_hash`'in ardına
     `AND s.created_at > v_born_after`; sınır bir `DECLARE` değişkenidir
     (`v_born_after timestamptz := pg_catalog.now() - interval '12 hours'`), çünkü kuşak taraması
     (`identitybelt_test.go`) bir WHERE bağlacında fonksiyon çağrısı ya da anahtar sözcük kabul etmez —
     00072'nin `v_fresh`'i emsaldir. Kilit altındaki yeniden okumalara (id ile) dokunulmaz: onlar bekleme
     sırasında işlenmiş bir iptale karşıdır; bir istek, çağrının başında okunan saatle yargılanır.
     İmzalar aynı (`CREATE OR REPLACE`; `SECURITY DEFINER`, `search_path`, sahip, `REVOKE … FROM PUBLIC`,
     `GRANT EXECUTE … TO tappa_app` yeniden yazılır — ADR 0031 §5.2 T5). Kapalı listeye fonksiyon
     **eklenmez**, CLAUDE.md §4.5 metni **değişmez**.
   - **`ListAdminSessionsForAdmin`** (çıkış/cihaz listesinin okuma yüzü) `revoked_at`'i çözümleyiciyle aynı
     kuralla döndürür: ömrü dolmuş bir oturum hiçbir listede canlı görünmez.
   - **Dokunulmayanlar, bilerek:** `identity_open_session` ve `identity_accept_invite_new` oturum **doğurur**
     (anahtar oturum değil); `identity_consume_reset` / `identity_consume_row_reset` yalnız iptal eder;
     iptal yazarları (`RevokeAdminSession*`, `identity_revoke_sessions`'ın döngüsü) süresi dolmuş satırı da
     damgalar — zararsız (tetikleyici damgayı monotonik tutar) ve 00085'ten önce de yaptıkları buydu.

3. **Yetki — tek değişiklik:** `admin_sessions` `SELECT (created_at)` → `tappa_resolver` ve
   `tappa_identitydefiner`. İkisinin zaten okuduğu bir tablonun var olan bir sütunu; ADR 0031 §5.2'nin
   matrisi `admin_sessions` için SELECT'i sütun listesiz adlandırır, yani definer matrisin içinde kalır
   (`identityADRMatrix`'in `"*"` hücresi); `tappa_resolver`'ın kimlik tablolarındaki yetkisi değişmez
   (`admin_sessions` kimlik tablosu değildir). Hiçbir fonksiyon sütunu döndürmez. `tappa_app` sütunu hâlâ
   yazamaz.

4. **Sıcak tabloda şema değişikliği yok:** sütun, indeks, veri yazımı yok; son **hesaplanır** (`revoked_at`
   toplu damgalanmaz, satır silinmez). Sütun GRANT'ı `admin_sessions`'ta kilit almaz (ölçüldü: `pg_locks`
   boş); `CREATE OR REPLACE FUNCTION` da. Down, bulduğu gövdeleri bayt bayt geri verir
   (`TestAdminSessionLifetime00085_DownGivesBackWhatItFoundAndUpHEAD`).

## Değerlendirilen seçenekler

- **`expires_at` sütunu.** Sıcak tabloya `ALTER` + geri doldurma ister; `created_at + sabit` aynı bilgiyi
  yazımsız verir. Reddedildi.
- **Yalnız Go'da (`Verify`) denetlemek.** Bir çağıranın unutmasına açık, ve `identity_*` definer'ları
  Go'nun çözümleyici cevabını görmez. Reddedildi (kart gereği 2).
- **Çözümleyicide süzmek (0 satır).** 00011'in "taşı, süzme" ilkesini bozar: bilinmeyen token ile biten
  oturum ayırt edilemez olur. Reddedildi; `revoked_at` sonu taşır.
- **Ömrü `authenticated_at`'ten saymak** (ADR 0031 §5.1 bunu *"ileride gelecek mutlak ömür"* için not etmişti:
  `NULL` = eski, fail-closed). Geçiş `authenticated_at`'i kopyalar, yani ömür geçişle uzamazdı; ama geçişte
  yazılan çerez 12 saat daha yaşar, tarayıcı ile sunucu ayrı anlarda biterdi — kullanıcı kararı *"çerezle
  aynı an"*. Ayrıca `tappa_resolver` `authenticated_at`'i okuyamaz (yeni hücre). Bu ADR'de seçilmedi; aşağıdaki
  açık soruya bakın.
- **Boşta kalma (idle) zaman aşımı.** Kullanıcı mutlak ömrü seçti; idle her istekte `last_used_at`
  karşılaştırması ve ayrı bir ürün kararı ister. Kapsam dışı.

## Sonuçlar ve kabul edilen riskler

- **Deploy anında** 12 saatten eski her iptal edilmemiş oturum biter; sahibi yeniden giriş yapar. Normal bir
  tarayıcı bu çerezleri zaten düşürmüştü — biten, saklanan ya da kopyalanan çerezdir.
- **Geçiş zinciri — açık soru (kullanıcıya).** Geçiş yeni satır doğurduğu için yeni bir 12 saat başlatır. En
  az iki üyeliği olan bir kimlikte, canlı bir çerezi tutan biri (meşru kullanıcı da, çalmış biri de) ömür
  dolmadan geçiş yaparak oturumu süresiz uzatabilir (gün başına bir geçiş yeter; geçiş kovası bunu
  sınırlamaz). Tek üyelikli kimlikte ve bağlanmamış satırda geçiş yoktur, ömür kesindir. Kapatmanın yolu
  ömrü ayrıca `authenticated_at`'e de bağlamak (giriş anından en çok N saat; `NULL` fail-closed) ve geçişte
  çerezin `Max-Age`'ini kalan süreye indirmektir — `tappa_resolver`'a `authenticated_at` hücresi ve bir
  ürün kararı (N) ister; bu ADR'de yapılmadı.
- **Saat:** gövdeler `now()` (transaction başlangıcı) okur; panel isteklerinin transaction'ları kısadır,
  sınırdaki fark milisaniyedir.
- Testler: `TestAdminSessionLifetime_TheResolverReportsTheEnd`, `TestAdminSessionLifetime_TouchRefusesAnEndedSession`,
  `TestAdminSessionLifetime_TheListShowsAnEndedSessionEnded`, `TestAdminSessionLifetime_ASwitchStartsAFreshLifetime`,
  `TestAdminSessionLifetime_EverySessionLookupInADefinerIsBounded` (yeni bir definer yüklemsiz bir hash
  araması eklerse kırmızı), `TestAdminSessionLifetime_EveryDefinerRefusesAnEndedSession` (her arama için
  sınırın iki yanı ve yüklemi kesilmiş gövdeyle kontrol), `TestVerify_EndsTheSessionAtItsLifetime`
  (`internal/adminauth`, uçtan uca).
