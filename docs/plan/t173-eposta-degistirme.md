# T173 — Panel kimliğinin e-posta değişikliği: kartlar

> **Durum:** kart yazıldı (2026-11-11, dal `m14-emailcard`); iki bağımsız inceleyici ONAY (turu 3,
> 2026-11-11); **kullanıcı kararları 2026-11-12: §10'un on dokuz önerisinin hepsi kabul** (ADR 0044'ün durum
> satırı); **EC-1 kodlanıyor.** Aşağıdaki kriterler önerilen seçenekleri varsaydığı için hiçbir madde yeniden
> yazılmadı. Canlı durum yalnız [state.md](state.md)'dedir.
>
> **Mimari karar:** [ADR 0044](../adr/0044-panel-kimliginin-e-posta-degisikligi.md) (Kabul edildi, 2026-11-12). Okumadan
> başlama: §5 (tasarım), §5.4 (düz zamanlama), §5.5 (açık linkler), §6 (geçiş), §10 (kararlar). Normatif
> zemin: [ADR 0031](../adr/0031-tek-kimlik-cok-isletme.md) §5.1, §5.2, §5.6, §5.8, §6.1, §8.
>
> **Aşağıdaki kabul kriterleri ADR 0044 §10'un ÖNERİLEN seçeneklerini varsayar.** Kullanıcı bir kararda
> başka seçeneği seçerse kart o maddede yeniden yazılır ve yeniden onaya gider; yapıcı sessizce uyarlamaz.

## Çalışma modu

- Yapıcı (`opus`) + ayrı üçüncü göz; her üç kart §4.5 (3)'e değer → **`tappa-security-auditor` zorunlu**.
  EC-1 agent `tappa-db-migrator`'ın kurallarıyla; EC-2'nin UI'ı ve e-posta şablonları skill `tappa-brand`'la.
- Testler (CLAUDE.md §10): tur içinde yalnız değişen testler `-run` ile + `gofmt`, build, `make gen` temiz,
  `bash scripts/redline-check.sh`; çapraz paket envanterleri her commit'ten önce
  (`go test ./cmd/tappa/ ./cmd/opadmin/ -run 'ConstantTime|Inventory|EveryNamedTest|StoreSurface|TablesLock' -count=1`).
  Tam `-race`, `go test ./...`, mutasyon seti ve `make check` kart başına **bir kez**, commit'ten önce.
- Henüz var olmayan testin adı belgeye yazılmaz (`TestEveryNamedTestExists`); test tarif edilir.
- **Migration numaraları:** **00101** (EC-1, şema + yetki), **00102** (EC-1, üç fonksiyon), **00103** (EC-1,
  süpürmenin genişlemesi). Başka numara gerekirse orkestratörden istenir.
- Dal: orkestratörün açacağı feature dalı; `main` birleştirmesi bir deploy kararıdır. EC-1 + EC-2 aynı
  deploy'da gider (§6: eski binary yeni şemada çalışır, yeni binary eski şemada koşmaz — migration önce).
- Bayrak: **yeni bayrak yok.** Rotalar `mountGated(SurfaceAccountIdentity)` altında (ADR 0044 §5.10).

## Görev listesi

| Kart | İş | Boyut | Bağımlılık |
|---|---|---|---|
| EC-1 | Şema, yetki, üç `identity_*` fonksiyonu, süpürme (00101–00103) + ADR 0031 §5.2 notu | L | ADR 0044 onayı + K-E1 |
| EC-2 | `adminauth` + handler + mektuplar + hesap sayfası + süpürmenin bağlanması | L | EC-1 |
| EC-3 | Runbook, ön/son kontroller, duman, M14-20b kartına ek | S | EC-1, EC-2 |

---

## EC-1 — Şema, yetki ve kapalı liste ekleri

- **Bağımlılık:** ADR 0044 kabul + K-E1 · goose 99 (bugün son `00099`). **`00100` (`m14-tagshape` şeridi) bu
  kartın değil ama ön koşuludur:** bu kartın deploy'undan önce ya da onunla aynı deploy'da gitmeli — madde 5'in
  00100 ↔ 00101 gidiş-dönüşü ve goose sırası ona dayanır; 00100 başka dalda kalırsa numara orkestratörle konuşulur
- **Kırmızı çizgi:** §4.5 (3) (kapalı listeye üç fonksiyon; var olan iki fonksiyon değişir — `identity_open_reset`'in imzası, `identity_unlink_backfilled`'ın gövdesi; matrise `UPDATE (email)`; K-E1 (ii) (a) ise §6.1 + CLAUDE.md metni) · §6 · §7
- **Araç:** agent `tappa-db-migrator` + güvenlik denetçisi
- **Commit:** `feat(db): let an identity change its sign-in address by mailbox proof`

**Dosyalar.** `db/migrations/00101_*.sql`, `00102_*.sql`, `00103_*.sql` · `internal/db/identity.go` (üç çağrı
+ süpürme + S-1: `OpenIdentityReset` ve `openIdentityResetSQL`, `internal/db/identity.go:324-381`) · `internal/db/identitycatalog_test.go`, `identitybelt_test.go`, `identityascii_test.go`,
`privcatalog_test.go`, `m11schema_test.go` (envanterler) · `internal/adminauth/identityreset.go` (S-1: çağıran
`openForIdentity`, alıcıyı geçer) · `docs/adr/0031-tek-kimlik-cok-isletme.md` §5.1 ve §5.2'ye tarihli not (kullanıcı
onayının tarihiyle) · K-E1 (ii) (a) ise `CLAUDE.md` §4.5 (3) ve ADR 0031 §6.1, bayt-aynı.

**Kabul kriterleri.**

*Şema (00101)*
1. `admin_identity_verifications`: `purpose` CHECK'i (adı korunur) `('verify','link','change','revert')`;
   `address citext NULL` ve `(address IS NOT NULL) = (purpose IN ('change','revert'))`; `cancelled_at
   timestamptz NULL`; TTL tavanı amaç başına (`'revert'` K-E3'ün süresi, öbürleri 24 saat). Adres biçimi
   CHECK'le **doğrulanmaz** (ADR 0031:261-265 — `DETAIL` sızıntısı).
2. `admin_identity_events_event_known` + `email_change_requested`, `email_changed`, `email_reverted`.
3. Yetkiler yalnız ADR 0044 §5.2'nin listesi: definer'a `admin_identities UPDATE (email)`, `admin_users
   UPDATE (email)`, doğrulama tablosunda `SELECT (address, cancelled_at)`, `INSERT (address)`, `UPDATE
   (cancelled_at)` ve — K-E1 (iii) (a) ise — `DELETE` (00103'ün süpürmesi). `tappa_app` ve `tappa_resolver`
   için **sıfır** yeni hücre (katalog testi bunu pinler).
   `admin_users` GRANT'ı kendi `SET LOCAL lock_timeout`'uyla; `admin_users` ALTER edilmez.
4. Ön koşul bloğu (00084'ün deseni): 00099 uygulanmış, tablolar ve `purpose` sütunu var; yoksa sabit mesajla
   durur, hiçbir şey değişmez.
5. **Down** — **00101, 00102 ve 00103'ün Down'larının her biri en başta aynı bloğu koşar** (goose Down'ları ters
   sırada ve ayrı transaction'larda koşar; blok yalnız 00101'de olsaydı `down-to 100` önce 00103 ile 00102'yi commit
   eder, geri alma fonksiyonunu düşürür ve `identity_unlink_backfilled`'ı korumasız bırakırdı — ADR 0044 §6): iki
   koşuldan **herhangi biri** (VEYA) tutarsa sabit bir mesajla durur ve hiçbir şey değiştirmez (00079'un Down'ı,
   `00079:471-476`): (1) canlı (süresi dolmamış, harcanmamış, iptal edilmemiş) bir `'revert'` satırı varken; (2)
   `admin_identity_events`'te `email_*` olayı varken — olay CHECK'i 00079'un biçimine geri konamaz, olay satırı
   silinemez (R17; tek silme yolu 90 günlük süpürme). Yoksa `'change'`/`'revert'` satırlarını siler, sütunları ve
   CHECK'leri 00084/00079'un biçimine **bayt bayt** geri koyar, yetkileri alır (pg_get_constraintdef + ACL md5'i
   100 ↔ 101 → Down eşit; 00100'ün bu ortamda uygulanmış olması gerekir).

*Fonksiyonlar (00102)* — ADR 0044 §5.3'ün imzaları birebir (dönüş sütunlarıyla); üçü ve yeniden tanımlanan
`identity_open_reset` de sahibi
`tappa_identitydefiner`, `SECURITY DEFINER`, `SET search_path = pg_catalog, pg_temp`, `REVOKE ALL … FROM
PUBLIC`, `EXECUTE` yalnız `tappa_app`, dinamik SQL yok.
6. `identity_request_email_change`: oturum canlı, 12 saatlik yüklem (00085), bağlı ve reddedilmemiş satır;
   kimlik kilidi altında `password_hash = p_verified_digest` değilse 0 satır; 6b yeni adreste (`IF (…) IS NOT
   TRUE THEN RETURN`); K-E9 (a)'nın üç ön koşulu (doğrulanmış adres, pencere kapalı, eski adreste bağsız aktif
   satır yok — kimliğin kendi adresiyle anahtarlı **adlı e-posta-geniş okuma**, `identityEmailWideReads`; tenant'ı
   önceden bilinmediği için açık `tenant_id` taşımaz) ve K-E9 (d) seçilirse canlı `'revert'` yok; citext'te kendi
   adresi. Oturum bulunamazsa 0 satır; her öbür ret **bir** satır, `refusal` ADR 0044 §5.3'ün yedi değerli kapalı
   kümesinden (`unverified`, `window_open`, `unlinked_rows`, `revert_live`, `same_address`, `invalid_address`,
   `stale`) — hiçbiri yeni adresin başka bir kimliğe/işletmeye ait olup olmadığına bakmaz (fonksiyon o soruyu
   sormaz). Önceki canlı `'change'` iptal, yeni satır, olay. Tenant tablosuna yazmaz, `audit_log`'a yazmaz.
7. `identity_change_email`: ADR 0044 §5.3'ün kilit sırası (adres kilidi → kimliğin sıfırlama danışma kilidi →
   kimlik → üyelikler → link → oturum) ve 1–8. adımları. "Yeni adreste kimlik yok" okuması `admin_identities`'i
   adresle anahtarlar — kemerin dördüncü sınıfı (`identityClaimAddressKeys`'in emsali), sayısıyla. Aynanın güncellemesi kimliğin `unlinked_at IS NULL` **her** bağlı satırına (devre dışı
   dahil), her ifadede `a.tenant_id = m.tenant_id`. `email_verified_at = now()`, onay penceresi **açılmaz**.
   Oturumlar K-E6 (a). Açık linkler ADR 0044 §5.5'in tablosu birebir. Her bağlı işletmeye bir
   `admin.email.changed` satırı: `{"outcome":"ok"}` + yalnız sıfır değilse `revoked_other_sessions`; adres,
   link id'si, `identity_id`, karşı işletme, sayı **yok**. **Bütün gövde** tek bir `EXCEPTION WHEN
   unique_violation THEN RETURN` bloğunda (`00084:430-434`'ün emsali; iç blok önceki yazıları bırakırdı) → 0 satır,
   hiçbir şey kalmaz.
8. `identity_revert_email_change`: ADR 0044 §5.3'ün gövdesi — madde 7'nin kilit sırası (eski adresin kilidi →
   sıfırlama kilidi → kimlik → üyelikler → link) ve **bütün gövdeyi saran** `unique_violation` bloğu; imza
   `identity_revert_email_change(p_revert_hash text)` — nöbetçi digest **yalnız SQL'de** üretilir (`00079:422`'nin
   ifadesi; güvenlik gereği — Go'dan gelen bir digest bcrypt CHECK'ine, `00069:180`, çarpıp adresi `DETAIL`'e
   basabilirdi); `confirm_opened_at` / `confirm_open_until` / `confirm_scope` tek UPDATE'te NULL; bağlı satırlara
   digest kopyası (contract B'ye dek); **bütün** oturumlar; kimliğin canlı sıfırlama, `'change'` ve öbür
   `'revert'` linkleri iptal; her işletmeye `admin.email.reverted` (aynı biçim). 0 satır: eski adres başka bir
   kimliğinse (dördüncü sınıf okuma) **ya da** kişinin bağlı herhangi bir işletmesinde eski adresli
   başka bir satır varsa (üyelik döngüsünde açık `tenant_id`; YÜKSEK-1 — kişinin reddettiği satır eski adreste
   kalır, `00072:903-905`, ve aynayı geri çevirmek `admin_users_tenant_email_key`'e çarpardı, `00006:96-97`).
9. Kimlik başına en çok **bir** canlı `'change'` ve **bir** canlı `'revert'` — kilit altında. K-E9 (d) (güvenlik
   gereği): canlı `'revert'` varken istek `refusal = 'revert_live'` döner (zincir oluşmaz).
9a. **`identity_unlink_backfilled` (YÜKSEK-1, K-E16 (a)):** aynı imzayla `CREATE OR REPLACE` (T5 kuralı aynen);
   kimlik kilidinden sonra kimliğin canlı bir `'revert'` satırı varsa 0 satır, hiçbir şey yazılmaz — "canlı"
   (`expires_at > v_now`) **kilitlerden sonra** `clock_timestamp()` ile yeniden okunan `v_now`'la değerlendirilir
   (00079'un deseni, `00079:155`); gövdenin geri
   kalanı 00079'unki bayt bayt (bir gövde-farkı testi bunu pinler). Down 00079'un tanımını ve ACL'ini bayt bayt
   geri koyar. K-E16 (b) seçilirse bu madde `identity_consume_reset` ve `identity_consume_row_reset`'e taşınır.
9b. **`identity_open_reset` (S-1):** dördüncü argüman `p_recipient text DEFAULT NULL` (`DROP` + `CREATE`; sahip,
   `SECURITY DEFINER`, `search_path`, `REVOKE ALL … FROM PUBLIC`, `GRANT EXECUTE … TO tappa_app` yeniden yazılır);
   kendi danışma kilidinden sonra `p_recipient IS NOT NULL AND i.email::text <> p_recipient` ise 0 satır; gövdenin
   geri kalanı 00072'ninki bayt bayt. Eski binary'nin üç argümanlı çağrısı `DEFAULT` ile çözülür ve bugünkü
   davranışı alır. Go tarafı: `internal/db/identity.go`'nun `OpenIdentityReset`'i yeni bir `recipient string`
   parametresi alır ve `openIdentityResetSQL` dört argümanla çağırır (`identity_open_reset($1, $2, $3, $4)`,
   `internal/db/identity.go:324-381`); `internal/adminauth`'un `openForIdentity`'si `identityRecipient`'ın adresini
   geçer; 0 satır "kimlik yok"un cevabını alır (yanıt ve taban aynı). Bir kaynak testi `openIdentityResetSQL`'in
   dört yer tutucu taşıdığını pinler (T295'in migration'ı `DEFAULT`'u kaldırana dek). Down 00072'nin tanımını ve
   ACL'ini bayt bayt geri koyar.

*Süpürme (00103)* — K-E1 (iii)'ün seçeneğine göre; aşağısı önerilen (a)'dır (süre K-E1 (iii)'ün süresi).
10. `identity_sweep_events()` **aynı imzayla** (`CREATE OR REPLACE`; sahip, `SECURITY DEFINER`, `search_path`,
    `REVOKE`/`GRANT` yeniden yazılır — T5) ve ek olarak harcanmış / iptal edilmiş / süresi dolmuş
    `'change'`/`'revert'` satırlarını 1 gün sonra siler; `'verify'`/`'link'` satırlarına dokunmaz. Definer'a
    doğrulama tablosunda `DELETE`. Down 00072'nin gövdesini bayt bayt geri koyar. (iii) (b) seçilirse dosya
    adresi NULL'layan yazarı ve CHECK değişikliğini taşır; (c) seçilirse 00103 doğmaz.

*Katalog ve kemer*
11. İmza tablosu, fiil × sütun matrisi, "her yetki kullanılıyor" egzersizi, `privCatalogAllowed`,
    `m11Functions`, operatör rollerinin yürütemediği fonksiyonlar listesi aynı commit'te genişler.
12. Kimlik kemeri üç gövdeyi ve yeniden tanımlanan `identity_open_reset`'i adıyla tarar; aile başına kontrol
    sayısı (T178 deseni, `identityBeltFamilies`). **Kemerin beş eki, her biri sayısıyla ve fonksiyonun adıyla:**
    (1) istekteki eski adres okuması `identityEmailWideReads`'e (`identitybelt_test.go:142`); (2) değişim ve geri
    almadaki adresle anahtarlı kimlik okuması dördüncü sınıfa (`identityClaimAddressKeys`, `:160-167`);
    (3) K-E1 (iii) (a) ise `identityPersonTableDeleters`'a (`:284-289`) `admin_identity_verifications →
    identity_sweep_events`; (4) aynı durumda `identityBeltAllowedCounts`'ta süpürmenin silme sayısı 1 → 2, ve
    "the named sweep over another table" kontrolü (`:1626-1627`) hâlâ kırmızı kalır (yine `admin_identity_resets`'i
    kullanır); (5) geri almanın SQL'deki nöbetçi digest'i: `identity_revert_email_change` için
    `identityBeltAllowedConstructs`'a `a call to public.crypt` / `public.gen_random_bytes` / `public.gen_salt` ve
    `identityBeltAllowedCounts`'a her biri 1 (`identity_unlink_backfilled`'ın girdisinin emsali,
    `identitybelt_test.go:249-253`, `:270`). Adres kuralı envanteri (`TestIdentityAddressRule_EveryIdentityInsertDecidesIt`) `admin_identities`'e
    `UPDATE … SET email` yazan her fonksiyonu da aday sayacak biçimde genişler — yeni iki yazarın reddi gerçek bir
    `IF (…) IS NOT TRUE` olmalı.
13. ADR 0031 §5.1'e (K-E1 (iii) (a) ise "silme yok"un adlandırılmış istisnaları, `0031:147`, `:154`, ve §5.2'nin
    `:332` satırı) ve §5.2'ye (kapalı liste, matris, `identity_open_reset`'in imzası, kemerin beş eki) tarihli
    not, kullanıcı onayının tarihiyle. K-E1 (ii) (a) ise §6.1 ve CLAUDE.md §4.5 (3) geri alma linkini adıyla sayar,
    iki metin bayt-aynı; (b) ise metin değişmez. Her iki durumda
    `TestClaudeMD_TheIdentityExceptionIsADR0031sSection61` yeşil.

**Testler (gerçek Postgres; adlar yapıcının).**
- Her ret dalı: 0 satır **ve** hiçbir tabloda yazı yok (öncesi/sonrası sayım) — bilinmeyen, iptal, harcanmış,
  süresi dolmuş link; başka kimliğin oturumu; 12 saati geçmiş oturum; digest uyuşmazlığı; doğrulanmamış adres;
  açık pencere; eski adreste bağsız satır; yeni adres bir kimliğin; kişinin bir işletmesinde aynı adresli satır;
  6b'den geçmeyen adres (U+0130, U+212A); K-E9 (d) ise canlı geri alma linki; geri almada kişinin bağlı bir
  işletmesinde eski adresli (reddedilmiş) satır → 0 satır, 500 yok, hata metninde adres yok.
- **YÜKSEK-1:** değişim → yeni adreste kimlik sıfırlaması (`reset` penceresi açılır) → `identity_unlink_backfilled`
  0 satır, satır bağlı kalır; geri almadan sonra `confirm_*` üçü NULL; K-E11'in sıfırlamasından sonra çözme
  çalışır.
- `refusal`: her ret dalı kendi değerini döner; yeni adresin üç durumunda (çakışmasız / bir kimliğin / kişinin
  bir işletmesinde satırı olan) `refusal` NULL ve dönüş sütun sütun aynı.
- 1, 2 ve 10 işletmeli kimlik: değişimden sonra her bağlı satırda ayna = kimlik adresi; devre dışı üyelik de;
  reddedilmiş satır eski adreste.
- Tetikleyici: değişimden sonra `tappa_app` ve `tappa_owner` bağlı satırın `email`'ini yazamaz (00069'un
  birinci kolu).
- Oturumlar: onaylayan kalır, öbürleri her işletmede kapanır; geri almada hepsi.
- Açık linkler: kimlik sıfırlaması ve bağlı satırların satır başı linkleri iptal ve tüketimleri 0 satır;
  `'verify'`/`'link'` linkleri dokunulmamış; eski adrese davet kabulü 0 satır, yeni adrese davet kabulü çalışır.
- K-E9 (d) ise: değişimden sonra ikinci istek 0 satır, geri alma linki sona erince yeniden mümkün. (d) değilse
  zincir: A→B→C, iki eski kutu → yalnız A'nın linki geri alır; geri almadan sonra her link iptal.
- Eşzamanlılık (`-race`, N goroutine): aynı link iki onay → tam bir başarı; aynı yeni adrese değişim + kayıt
  sahiplenmesi + davet kabulü → tam biri o adresin kimliği olur, 500 yok, hata metninde adres yok;
  değişim + parola sıfırlama tüketimi → kilitlenme yok; **S-1:** `identity_open_reset` (alıcıyla) ↔ değişim ve
  ↔ geri alma, N tur → her turda eski adrese canlı sıfırlama linki kalmaz (ya iptal edilmiş ya hiç basılmamış),
  kilitlenme yok; **S-2:** geri alma ↔ `identity_switch_session` / `identity_open_session` /
  `identity_link_by_email` → sonunda kimliğin canlı oturumu 0.
- Audit biçimi: satır anahtarları tam `outcome` (+ sıfır değilse sayı); hiçbir audit `detail`'inde adres, hiçbir
  işletmede karşı işletmenin id'si.
- RLS izolasyonu: tenant A'nın bağlantısı (filtresiz sonda) değişimden sonra B'deki satırı görmez.
- Down: 100 ↔ 101 ↔ 102 ↔ 103 gidiş-dönüş; canlı geri alma linkiyle ve ayrıca bir `email_*` olayıyla
  `goose down-to 100` **00103'te** durur ve üç dosyanın hiçbiri geri alınmaz (her Down'ın bloğu ayrı ayrı mutasyonla
  kırmızı).

**Mutasyon kanıtları (her biri: kaldır → kırmızı → geri al; raporda listelenir).**
oturumun 12 saat yüklemi · digest yeniden denetimi · 6b reddi (iki yazarda ayrı) · "yeni adreste kimlik yok"
yeniden denetimi · aynı işletmede aynı adresli satır denetimi (değişimde) · **geri almada aynı işletmede eski
adresli satır denetimi** · aynanın `tenant_id` yüklemi · aynanın `identity_id` yüklemi · `unique_violation`
yakalaması **iki fonksiyonda ayrı ayrı** ve "iç bloğa daraltılmış" mutantı (önceki yazılar kalır → kırmızı) ·
kimlik sıfırlamalarının iptali · satır başı linklerin iptali · "tek canlı `'revert'`" kuralı ya da K-E9 (d)'nin
reddi · `identity_unlink_backfilled`'ın canlı geri alma linki yüklemi · o yüklemin saati (kilitten **önce** okunan
saate çevrilince, kilit beklerken süresi dolan link testi kırmızı) · geri almanın `confirm_*` kapatması ·
geri almada "eski adreste kimlik yok" · K-E9'un ön koşulları (ayrı ayrı) · `identity_open_reset`'in
alıcı yüklemi · değişim/geri almanın sıfırlama danışma kilidi (kaldırılınca S-1 yarışı kırmızı) · audit
`detail`'inin sıfırda sayıyı düşürmesi · süpürmenin `'verify'`/`'link'`'e dokunmaması · Down'ın iki durması.

---

## EC-2 — Uygulama: istek, onay, bildirim, geri alma

- **Bağımlılık:** EC-1
- **Kırmızı çizgi:** §4.5 (3) · §4.6 · §7 · §9 (hesap sayfası; tap/sonuç ekranına dokunulmaz)
- **Araç:** yapıcı (skill `tappa-brand`) + güvenlik denetçisi
- **Commit:** `feat(panel): change the sign-in email from the account page`

**Dosyalar.** `internal/adminauth/` (yeni dosya; `identityaccount.go`'nun deseni: iki yeni türetilmiş HMAC
anahtarı — değişim ve geri alma, `deriveVerifyKey`'in biçimi) · `internal/handler/` (hesap sayfası formu,
istek, link inişi, onay, geri alma inişi + onayı; ayrılmış mektup kuyruğu) · `internal/handler/adminratelimit.go`
(yeni sabitler, `TestPanelConstants_ShippedValuesArePinned`'e) · `web/templates/` (hesap kartı, onay ve geri alma
sayfaları, mektuplar: değişim linki, çakışma bilgisi, eski adres bildirimi linkli ve linksiz, K-E14 (b) ise
yeni adres onayı) · S-1'in Go yarısı EC-1 9b'dedir (`internal/db/identity.go`, `internal/adminauth/identityreset.go`) · `cmd/tappa/main.go`
(süpürmenin periyodik çağrısı — T294).

**Kabul kriterleri.**

*İstek*
1. Hesap sayfasında, yalnız bağlı kimlik oturumunda, **"Change your sign-in email"** kartı (K-E10): yeni adres +
   mevcut parola + formun uyarısı *"Your new email will be visible to the admins of every business you belong
   to."* (K-E10 (ii), ADR 0044 §8 risk 10). Rotalar K-E15'in zincirinde — önerilen (a): `ProtectWriting`'in
   askı halkalı ana grubu; (b) seçilirse `protectWritingThroughSuspension`'ın grubu ve
   `TestSuspensionRoutes_EveryPanelWriteIsGatedOrANamedException` + ADR 0025'in listesi aynı commit'te. Bağlanmamış satırın oturumu kartı görmez ve rotası `unavailable` döner (ücretsiz — kişinin
   kovasını harcamaz).
2. Sıra: iş kapısı → kişinin kovası (`TryCharge`, karşılaştırmadan önce) → adres biçimi (`mail.ValidRecipient`,
   kopya değil) → mevcut parola (`identity_password_for_session`'ın digest'i, `changepassword.go`'nun deseni) →
   `resolve_identity_by_email(yeni)` (yalnız mektubu seçmek için) → `identity_request_email_change` →
   mektubun kuyruğa bloklamadan sunulması → **taban** → 303 + aynı flash. **Önceden yalnız kişinin kovası
   ayrılır;** alıcı tavanının önceden ayırma yolu yoktur (`mail.RecipientCap`'te yalnız `Capped` ve `Send`,
   `internal/mail/recipientcap.go:263`, `:278`) — tavan kuyruktaki işçinin `Send`'inde sayılır.
2b. **Flash (K-E10 (i)):** (a) önerilen: kapalı sözcük listesinden (`accountactions.go:311-319`), metin adres
   anmaz; adres hiçbir URL'ye, `Location`'a, log'a girmez (test: 303'ün `Location`'ı ve sonraki GET'in
   gövdesi adresi taşımaz). (b) seçilirse: dar yollu, kısa ömürlü, `HttpOnly`, `SameSite=Strict` bir çerez, bir
   kez okunur ve silinir; aynı test + çerezin yolu/ömrü pinli.
3. Yanlış parola: görünür ret (*"Your current password is not right"*) — kişinin kendi bilgisi; kova harcanır.
   `revert_live`'ın tarihi K-E18'e göre (a: ret satırının `expires_at`'i; b: tarihsiz cümle).
   Definer'ın `refusal` değerleri K-E10 (v)'in cümlelerine birebir eşlenir (kapalı küme, bilinmeyen değer 500
   değil genel ret); eşlemeyi bir tablo testi pinler. Hiçbir cümle yeni adres hakkında konuşmaz.
4. **Düz zamanlama:** çakışmasız / yeni adres bir kimliğin / yeni adres kimsesiz ama kişinin bir işletmesinde
   satırı var — üç kol aynı yanıtı (gövde baytları, durum, başlıklar; oturuma özgü değerler hariç) ve aynı
   tabanı öder; HTTP üzerinden gerçek Postgres'le iç içe ölçüm, medyanlar aynı bantta (biçim:
   `TestPanelIdentityReset_TimingIsFlatOverHTTP`). Taban her çıkışta uygulanır (yapısal test).
5. Mektup kuyruğu sıfırlama outbox'ından **ayrıdır** (sınırlı, süreç içi; dolunca o mektup gönderilmez ve bir
   log satırı yazılır, yanıt değişmez); işçi panikleri yakalar, panik değerini log'a yazmaz (ADR 0022 §6.4'ün
   deseni). Çakışmasız kolda değişim linki, çakışma kolunda linksiz bilgi mektubu (K-E5); ikisi de işçide
   `RecipientCap.Send` ile (kapsam kişinin kovası — tavan burada sayılır ve reddeder) ve kesiciden `Send` ile.

*Onay listesi (K-E16 / K-E19)*
5b. Geri alma linki yaşarken onay listesinde "Remove"a basan kişi K-E19'un yüzünü görür — önerilen (a): onay
   listesinin genel ret cümlesine *"If you changed your sign-in email in the last 7 days, removing a business waits
   until then."* eklenir (yeni taşıyıcı yok). (b) seçilirse düğme gizlenir ve `identity_confirm_window` değişir
   (kapalı liste, EC-1'e madde). Test: değişimden sonra "Remove" bu cümleyi gösterir, satır bağlı kalır.

*Onay*
6. Link GET'te harcanmaz: değer kısa ömürlü, dar yollu bir çereze girer ve tarayıcı hesap sayfasına gider
   (`verifyLinkLanding`'in deseni); oturum yoksa giriş önce, çerez bekler. Sayfa onay düğmesini adres
   **anmadan** çizer (K-E10; çözümleyici eklenmez).
7. Onay POST'u `identity_change_email`'i link hash'i + çerezin oturum hash'i + yeni geri alma linkinin hash'iyle
   çağırır; ret tek sayfa (*"This link can't be used…"*), dalları ayırt etmez.
8. Başarıda: eski adrese **bir** bildirim (passwordnotice deseni — istek içinde, outbox'tan değil,
   `SendExempt`, kendi gönderim süresi sınırı, kimlik başına saatte 5); K-E2 (a) ise mektup geri alma linkini
   taşır (`revert_expires_at` doluysa); NULL ise (zincir, yalnız K-E9 (d) seçilmezse) **linksiz** biçim, K-E10
   (vi)'nın ikinci metni — "within 7 days" yalnız linkli biçimde. K-E8 (a) yeni adresi anmaz. **Yeni adrese onay
   mektubu yalnız K-E14 (b) seçilirse** — aynı yoldan, linksiz, eski adres bildirimiyle tek kimlik tavanını
   paylaşır. Bildirim başarısız olursa değişiklik durur ve yanıt yine başarıdır; sonuç bütçeli bir log satırıdır
   (audit değil — kimliğin bildirimi hiçbir işletmenin değildir).

*Geri alma*
9. Geri alma linki GET'te harcanmaz (aynı desen, kendi çerezi); oturum **istemez** — rota
   `mountGated(SurfaceAccountIdentity)` altında ama panelin oturum isteyen grubunun **dışında**, sıfırlama ve
   davet sayfaları gibi kendi bağlamasıyla (ADR 0031:692-693); oturumlu bir tarayıcıda da oturuma bakmaz. Onay
   sayfası ne olacağını söyler (eski adres geri gelir, her yerde çıkış, yeni parola linki). POST'ta IP kovası
   (`adminResetSubmitLimit`'in deseni) → `identity_revert_email_change` → başarıda eski adrese
   `identity_open_reset` ile bir sıfırlama linki (K-E11 (a); `p_recipient` = geri almanın döndürdüğü adres, S-1;
   M14-14'ün yolu) → sabit bir sonuç sayfası. Ret tek sayfa. Olgu (karar değil): oturumsuz rota askı halkasının
   dışındadır, ayna askıdakiler dahil her işletmeye yazılır, definer askıya bakmaz (ADR 0044 §5.3).
9b. **S-1'in Go yarısı:** `openForIdentity` her çağrıda `identityRecipient`'ın adresini `OpenIdentityReset`'e
   geçer; 0 satır "kimlik yok" gibi cevaplanır (sayfa ve taban aynı, bir log satırı). Test (gerçek Postgres):
   resolver'dan sonra, açılıştan önce bir değişim commit edilir → link basılmaz, eski adrese mektup gitmez.

*Güvenlik ve veri*
10. Token'lar yalnız mektuba girer; hiçbir log, yanıt, hata metni ya da veritabanı satırında değil (veritabanı
    hash'i tutar). İki yeni HMAC anahtarı birbirinin ve doğrulama/link anahtarlarının hash'ine çözülmez (test).
11. Log anahtarları ADR 0022 §10 + `identity_id`; adres hiçbir satırda (her harf büyüklüğünde), panik değeri yok;
    röle davranışlarının hepsinde (biçim: `TestPasswordNotice_LogsOnlyIdsClassAndCode`).
12. Mektuplar ADR 0022 §4 (sabit ASCII konu, alıcı kuralı) ve §8 şablon kurallarıyla; tek link
    `TAPPA_BASE_URL`'den, istek başlığından asla (resetmail.go'nun kuralı; düşman `Host` /
    `X-Forwarded-Host` ile test).
13. Her POST panelin CSRF/Origin + `tb` bağıyla (M14-10a); geri alma ve link inişleri sıfırlama akışının
    bağlamasıyla.
14. Süpürme `cmd/tappa/main.go`'da taslak süpürmesinin yanına bağlanır (sabit periyot); çağrıldığı bir testle
    pinlenir (T294'ü kapatır).

*Doğrulama*
15. Her yeni korumanın mutasyon kanıtı: taban · kova önce · bağlanmamış satırın ücretsiz reddi · kuyruğun
    ayrılığı (sıfırlama outbox'ı doluyken mektup yine kuyruğa girer) · GET'in harcamaması (iki iniş) · oturum
    şartı (onay) · `SendExempt` (kesici açıkken bildirim gider) · HMAC anahtar ayrılığı · log anahtar kümesi ·
    flash'ın adres taşımaması · `openForIdentity`'nin alıcıyı geçmesi (kaldırılınca 9b'nin testi kırmızı) ·
    linksiz bildirim biçiminin seçimi.
16. Gerçekçilik (kullanıcı kuralı "piyasa deseni önce"): raporda her görünür adımın ana akım karşılığı
    (Google/Microsoft/GitHub: mevcut parola, yeni adrese doğrulama, eski adrese "bu sen değilsen" bildirimi).

---

## EC-3 — Canlıya alma

- **Bağımlılık:** EC-1, EC-2 · **Araç:** yapıcı + üçüncü göz (güvenlik denetçisi EC-1/EC-2'ninkini kapsar)
- **Commit:** `docs(deploy): runbook for the sign-in email change`

**Kabul kriterleri.**
1. `deploy/README.md`'ye bölüm: ön kontroller (salt okuma: goose **100** — 00100 ön koşuldur, EC-1;
   `admin_identity_verifications`'ta yalnız `'verify'`/`'link'`, adresi kimliğinkinden farklı bağlı satır = 0),
   deploy sonrası kontroller (goose 103; üç yeni fonksiyonun sahibi/ACL'i/`search_path`'i; `identity_open_reset`'in
   **dört argümanlı** imzası, sahibi, ACL'i ve **üç argümanlı eski tanımın artık olmadığı**
   (`to_regprocedure('public.identity_open_reset(uuid,text,timestamptz)')` NULL); `identity_unlink_backfilled`'ın
   yeni gövdesi ve ACL'i; `tappa_app`'ın `admin_users.email`'e hâlâ yazamadığı; `identity_sweep_events`'in yeni
   gövdesi) ve duman (iki işletmeli kimlik: değiştir → iki işletmede ayna ve
   audit → eski adrese bildirim → geri al → yeni parola linki → giriş).
2. Geri dönüş paragrafı: önceki imaj; Down ancak sonraki deploy'da ve iki durmanın **ikisi de** tutmuyorken —
   canlı geri alma linki yok **ve** `admin_identity_events`'te `email_*` olayı yok (olaylar ancak 90 günlük
   süpürmeyle yaşlanır, T294 bağlandıktan sonra); salt okuma sayım sorgusu bölümde (ADR 0044 §6).
3. `docs/plan/m14-tek-kimlik.md` M14-20b'nin 1b listesine `identity_revert_email_change` (digest kopyalar →
   contract B'den sonra `NULL`) ve ADR 0031 §8 adım 8'e aynı satır — kullanıcı onaylı ADR 0044'e atıfla.
4. Haftalık sayımlar bölümüne (varsa) bir satır: canlı geri alma linki sayısı (salt okuma, adres basmadan).
