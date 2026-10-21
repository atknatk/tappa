# M14 — Tek kimlik, çok işletme: panel yöneticisinin global kimliği ve işletme değiştirici

> **Durum:** plan — **başlanmadı; kullanıcının *"başla"* demesini bekliyor.** Kararlar 2026-10-21'de
> verildi (K-1 … K-14 hepsi (a)); aynı gün iki denetimden (33 bulgu) sonra kullanıcı **K-13'ü (b)'ye
> çevirdi** (üçüncü açık §4.5 istisnası). Tek açık soru K-15 (e-posta değiştirme kapsamı) ve M14-01'de
> sorulur. Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya "yapıldı" yazılmaz.
>
> **Mimari karar:** [ADR 0031](../adr/0031-tek-kimlik-cok-isletme.md) (kabul edildi 2026-10-21).
> Okumadan başlama: §5 (tasarım), §6 (üçüncü istisna ve CLAUDE.md metni), §8 (geçiş), §13 (kararlar ve
> denetim düzeltmelerinin anlamlarına etkisi).

## Özet

- **Kişi** — tenant-kapsamsız beş tablo (`admin_identities`, `admin_identity_resets`,
  `admin_identity_verifications`, `admin_signup_claims`, `admin_identity_events`); uygulama
  göremez; yalnız kapalı `identity_*` listesi (`tappa_identitydefiner`) ve salt okuma
  çözümleyiciler (`tappa_resolver`).
- **Üyelik** = bugünkü `admin_users` + `identity_id` + `linked_via` (yalnız definer yazar); `id`
  değişmez. Bağlı satıra doğrudan parola/e-posta yazması tetikleyiciyle reddedilir; dual-write kimlik →
  satırlar.
- **Oturum** = bugünkü `admin_sessions` (+ `authenticated_at`), tek tenant'a damgalı. Geçiş = yeni
  oturum, eski iptal, sıradan giriş/çıkış satırlarıyla bayt-aynı iki audit satırı; oran kovası kimliğe
  anahtarlı. Her panel formu tenant'a bağlı (eski sekme yanlış işletmeye yazamaz).
- **Birleştirme** = iki canlı oturum (iki kimlik bilgisi) — veritabanında yapısal.
- **Kayıt** = doğrulama tıklamasından önce hiçbir şey doğmaz (M14-17, bayrağın ön koşulu); yeni adreste
  onay formda seçilen parolayı yeniden ister.
- **Onay penceresi** = ilk doğrulamada ve her sıfırlamada kişi backfill/uzlaştırma/kayıtla bağlanmış
  üyelikleri onaylar ya da çözer; çözülen satır kalıcı ret + kullanılamaz digest alır.
- **Bayrak** = bağlı ve bağlanmamış oturumun farklı görebileceği her yüzeyi kapatır (ADR 0031 §8 tablosu).
- **Geçiş** = expand (bayrak `off`) → uzlaştırma → P5 kartları (M14-17 dahil) → bayrak `on` (05-config
  commit + deploy) → 60 gün soak → contract A (kod) → contract B (şema).

> ⏱ **Kullanıcıya not (R23):** P5 kartlarının (sıfırlama, davet, kayıt doğrulaması, hesap sayfası)
> bayraktan **önce** canlıda olması güvenlik için şarttır (ADR 0031 §5.10, §8). Bedeli: **"çıkış
> yapmadan işletme değiştirme" kullanıcıya ancak P5 bittikten sonra açılır** — kabaca expand'den sonra
> P5'in süresi kadar (tek hatla ~15–20 iş günü) gecikir. Geçiş kodu o sürede canlıdadır ama kapalıdır.
>
> 🔁 **Kullanıcıya not (T11):** her parola sıfırlamasından sonra kişi, üyeliklerinin tam listesini bir
> onay ekranında görür (güvenlik: kimliğin denetimi el değiştirmiş olabilir). Varsayılan tek tıklık
> **"Keep all"**dır; bir işletmeyi çözmek bilinçli, satır satır bir eylemdir.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz**, bulgu varsa yeni denetçiyle yeniden; onaysız `done` yok. Bu
  milestone'un neredeyse her kartı §4.5'e değer → `tappa-security-auditor` varsayılan (istisnalar
  kartta yazılı). Her yeni tablo/sorgu/fonksiyon **`tappa-db-migrator`**; her UI işi skill
  **`tappa-brand`**.
- **Testler:** tur içinde yalnız değişen testler `-run` ile + saniyelik kapılar (`gofmt`, build,
  `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...`, mutasyon seti ve `make check` kart
  başına **bir kez**, commit'ten önce. Brief'lere yazılır.
- `make gen` her `.templ` / `db/queries/*.sql` değişikliğinden sonra; üretilen dosyalar commit edilir.
- Uygulanmış migration değişmez (§6); bugün son `00063`. **Migration numaraları (R20):** M14 toplam
  **on bir** dosya doğurur — M14-03 iki (N: tablo + sütunlar + tetikleyici; N+1: `CONCURRENTLY` indeksi),
  M14-04 bir (N+2), M14-05 bir (N+3), M14-06 bir (N+4), M14-06b bir, M14-15 bir, M14-16 bir, M14-17 bir,
  M14-19 bir (yalnız `REVOKE EXECUTE`), M14-20b bir (M14-20a şemaya dokunmaz; M14-08…M14-14 ve M14-18 migration doğurmaz — doğurursa kart
  numara ister). **Paralel şema hattı için N … N+4 beş numara**, M14-03 açılırken orkestratörce birlikte
  ayrılır (bugün N = `00064`, o gün başka bir migration girmediyse); kalanlar kart açılırken sırayla.
- Henüz var olmayan testin adı belgeye yazılmaz (`TestEveryNamedTestExists`); testi tarif et.
- **Dal:** kullanıcının açacağı feature dalı (öneri `m14-identity`). Her `main` birleştirmesi bir
  deploy kararıdır — expand, uzlaştırma, bayrak, contract A ve contract B **ayrı** birleştirmeler.
- Kart bitince: `state.md` ledger + "ŞU AN" + oturum notu.

## Görev listesi

Boyut: **S** ≈ yarım gün · **M** ≈ 1 gün · **L** ≈ 2–3 gün · **XL** ≈ 4+ gün (bir tur yapım; bu repoda
güvenlik kartları 3–6 denetim turu sürdü, aşağıdaki toplam bunu içerir).

| ID | Faz | Görev | Boyut | Araç / ajan | Bağımlılık |
|---|---|---|---|---|---|
| M14-01 | P0 | CLAUDE.md §4.5/§6/§3 (ADR 0031 §6.1 birebir) + ADR 0002/0013/0015/0025 notları + K-15 sorusu + backlog notları | S | orkestratör | kullanıcı "başla" |
| M14-02 | P0 | Canlı salt sayı ölçümü | S | yapıcı + kullanıcı | — |
| M14-03 | P1 | Rol + beş tenant-kapsamsız tablonun ilki (`admin_identities`) + `admin_users.identity_id/linked_via` + tetikleyici + `UPDATE (email)` REVOKE + `admin_sessions.authenticated_at` + katalog listeleri | L | `tappa-db-migrator` + güvenlik | M14-01 |
| M14-04 | P1 | Çözümleyiciler + oturum/üyelik/geçiş/toplu iptal fonksiyonları + imza ve matris testleri | L | `tappa-db-migrator` + güvenlik | M14-03 |
| M14-05 | P1 | Parola/sıfırlama/bağlama (tazelik)/çözme (nöbetçi digest)/doğrulama (oturuma bağlı)/olay fonksiyonları + `admin_identity_resets`, `admin_identity_verifications`, `admin_identity_events` | XL | `tappa-db-migrator` + güvenlik | M14-03 |
| M14-06 | P1 | Backfill (en eski aktif önce, idempotent, rol iddiası) | M | `tappa-db-migrator` + güvenlik | M14-02, M14-03 |
| M14-06b | P1 | Uzlaştırma (idempotent; expand'den sonraki deploy'da ve M14-19'dan önce) | S–M | `tappa-db-migrator` + güvenlik | M14-06 |
| M14-07 | P2 | `adminauth`: kimlik doğrulaması, eski kol, düz zamanlama (1 + 8, beş durum), iki taze oturumlu otomatik bağlama | L | yapıcı + güvenlik | M14-04, M14-05 |
| M14-08 | P2 | `adminauth`: Open/Switch/RevokeAll/ChangePassword + bayrak (`05-config.yaml`) ve kapılı yüzeyler tablosu + dual-write (kayıt, parola, eski sıfırlama linki) | L | yapıcı + güvenlik | M14-07 |
| M14-09 | P3 | Giriş handler'ı: kimlik yolu, iniş, eski kolda seçici, audit sayı sızıntısının kapanması | L | yapıcı + güvenlik | M14-08 |
| M14-10a | P3 | Panel formlarının tenant bağı — sorgu parametresinde, ham oturum token'ından türetilmiş anahtarla (eski sekme koruması) | L | yapıcı + güvenlik | M14-08 |
| M14-10 | P3 | `GET/POST /admin/switch`: CSRF, kimlik kovası, askı grubu, eşzamanlılık, bayt-aynı audit | M | yapıcı + güvenlik | M14-10a |
| M14-11 | P3 | Her yerden çık + parola değişiminde bütün işletmelerde iptal | S–M | yapıcı + güvenlik | M14-08 |
| M14-12 | P4 | Kabukta işletme adı + "Switch business" (S1/S7, üç genişlik) | M | yapıcı (`tappa-brand`) | M14-10 |
| M14-13 | P4 | Değiştirici sayfası + Choose metni | S–M | yapıcı (`tappa-brand`) | M14-10 |
| M14-14 | P5 | Kimlik başına sıfırlama (tek link) | L | yapıcı + güvenlik | M14-05, M14-08 |
| M14-15 | P5 | Yönetici daveti (`admin_invites`, M7-07/EM-12'yi devralır) | XL | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-08, M14-14 |
| M14-16 | P5 | Üyeliği devre dışı bırak, sahiplik devri, son-owner değişmezi | M–L | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-15 |
| M14-17 | P5 | Kayıt: e-posta doğrulaması (EM-11), `admin_signup_claims`, var olan adres | XL | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-08, M14-14 |
| M14-18 | P5 | Hesap: "Your businesses", e-posta doğrulama, ilk doğrulama onay listesi, "Link a business" | L | yapıcı (`tappa-brand`) + güvenlik | M14-05, M14-07, M14-12 |
| M14-19 | P6 | Bayrağı açma (05-config commit + deploy, ön kontroller, migration: onay pencerelerinin taşınması + `identity_create_for_signup` EXECUTE REVOKE, izleme sayaçları) | S–M | yapıcı + `tappa-db-migrator` + kullanıcı | M14-06b, M14-09 … M14-13, M14-14, M14-17, M14-18 |
| M14-20a | P6 | Contract A: binary eski kolu bırakır, bayrak kalkar, şema aynı | L | yapıcı + güvenlik | M14-19 + K-3 + ön koşullar |
| M14-20b | P6 | Contract B: tetikleyici değişimi, digest kopyalayan definer gövdelerinin aynı imzayla yenilenmesi, DROP/REVOKE/NULL, `created_at`'li VALID CHECK (sonraki deploy) | L | `tappa-db-migrator` + güvenlik | M14-20a canlıda |
| M14-21 | P7 | Faz sonu doğrulaması | M | denetçi + güvenlik | hepsi |

**Toplam kaba tahmin:** tek hatla **~39–54 iş günü** (denetim turları dahil; ikinci tur M14-05'i XL'e,
üçüncü tur M14-20b'yi L'ye çıkardı); üç paralel hatla ~25–33. Takvimde buna K-3'ün **60 günlük soak'u** eklenir (contract ondan önce olamaz).

### Paralel hatlar ve deploy sınırları

```
P0  M14-01 ─┬──────────────────────────────────────────────────────────────────
    M14-02 ─┤ (kullanıcıyla)
P1          └─ M14-03 ─┬─ M14-04 ─┐
                       ├─ M14-05 ─┼─ M14-07 ─ M14-08 ─┬─ M14-09
                       └─ M14-06 ─┘                   ├─ M14-10a ─ M14-10 ─┬─ M14-12
                                                      ├─ M14-11            └─ M14-13
                       ═══ DEPLOY 1: EXPAND (bayrak off) ═══  (P1 + P2 + P3 + P4)
                          M14-06b ═══ DEPLOY 2: UZLAŞTIRMA ═══
                                                      ├─ M14-14 ─┬─ M14-15 ─ M14-16
                                                      │          └─ M14-17
                                                      └─ M14-18
                       ═══ P5 kartları tek tek, bayrak off ═══
                          M14-19 ═══ DEPLOY: BAYRAK ON (05-config) ═══   ← M14-17 şart
                          soak (K-3: bayrak açıldıktan 60 gün)
                          M14-20a ═══ CONTRACT A ═══ → M14-20b ═══ CONTRACT B ═══
P7  M14-21
```

- **Hat A (şema):** M14-04, M14-05, M14-06 M14-03'ten sonra paralel (M14-03 açılırken birlikte ayrılan
  N … N+4 beş migration numarası; çalışma modu).
- **Hat B (oturum):** M14-09, M14-10a, M14-11 M14-08'den sonra paralel; M14-10 M14-10a'yı, M14-12/13
  M14-10'u bekler.
- **Hat C (e-posta):** M14-14 → M14-15 → M14-16; M14-17 ve M14-18 paralel. Hat C expand deploy'unu
  bloklamaz ama **M14-17 bayrağı bloklar** (ADR 0031 §5.10).

---

## Kararlar (2026-10-21)

Kullanıcı 2026-10-21'de *"Önerilerin hepsini kabul"* dedi (K-1 … K-14 hepsi (a)); aynı gün iki
denetimden sonra **K-13'ü (b)'ye çevirdi**. Denetim düzeltmelerinin bir kararın anlamını değiştirdiği
yer kararın yanında yazılıdır (ADR 0031 §13 tam tablo).

1. **K-1 — E-posta global tekil (kimlik başına).** (a) seçildi.
2. **K-2 — Birleştirme yalnız iki kimlik bilgisinin kanıtıyla.** (a) seçildi. ⚠️ *Anlam sıkılaştı
   (bulgu 9):* kanıt veritabanında **iki canlı oturumdur** (`identity_link_legacy`); e-posta eşitliği
   yalnız savunma katmanı.
3. **K-3 — Bağlanmamış satırlar için son tarih, bayrak açıldıktan 60 gün sonra.** (a) seçildi.
   *Anlam değişmedi* (bulgu 20 + R5, orkestratör kararı): saat = `max(bayrak, M14-14 ve M14-18 canlı)`;
   yeni sırada ikisi bayraktan önce canlıda → saat **bayrağın açıldığı gün** başlar, kullanıcının kabul
   ettiği metin. Ek contract ön koşulu: *"tek aktif owner'ı bağlanmamış tenant = 0"*; reddedilmiş
   satırların tenant'ları ayrı raporlanır (bulgu 13, R1).
4. **K-4 — Girişte son kullanılan işletme, seçici yok.** (a) seçildi. ⚠️ *Anlam daraldı (bulgu 10):*
   "seçici yok" kimlik yoluna aittir; eski kolda birden çok satır doğrularsa seçici contract'a dek kalır.
5. **K-5 — Kabukta işletme adı + "Switch business" bağlantısı, liste sayfada.** (a) seçildi.
6. **K-6 — Devre dışı bırakma yalnız üyelik başına.** (a) seçildi. ⚠️ *Anlam değişti (bulgu 24):*
   "kimlik kilidi yalnız sistemin" kalktı — M14'te **kimlik kilidi yok** (`status` sütunu yok).
7. **K-7 — Var olan kimliği başka işletmeye yalnız owner, davetle ekler; davetli kendi parolasıyla
   kabul eder.** (a) seçildi. ⚠️ *Sıkılaştı (bulgu 12, R12, R18):* kimliğin e-postası davet e-postasına
   eşit olmalı; kabul oturum vermez; owner adresin kimliği olup olmadığını öğrenmez; davet yüzeyi
   bayrakla kapılı. *Orkestratör netleştirmesi (2. tur):* var olan müşterilere önce sıfırlama
   **dayatılmaz** — davet linkine aynı e-postalı kimliğin canlı oturumuyla tıklamak (POST) posta kutusu
   kanıtıdır, `email_verified_at`'i doldurur ve onay penceresini açar.
8. **K-8 — Rol işletme başına.** (a) seçildi.
9. **K-9 — Kayıt e-posta doğrulamasıyla biter; var olan adres aynı ekranı görür.** (a) seçildi.
   ⚠️ *Genişledi (bulgu 7, 11, R2):* tıklamadan önce kimlik/tenant/üyelik doğmaz; yeni adreste onay
   formda seçilen parolayı yeniden ister; M14-17 bayrağın ön koşulu. *Orkestratör netleştirmesi:* var
   olan adreste tıklama, aynı e-postalı kimliğin canlı oturumuyla posta kutusu kanıtıdır (K-7 gibi).
10. **K-10 — Askıdaki işletme değiştiricide işaretli, geçiş serbest.** (a) seçildi.
11. **K-11 — Sahiplik devri ve üyelik kaldırma M14'te.** (a) seçildi.
12. **K-12 — Operatör değişmez.** (a) seçildi. ⚠️ *Genişledi (bulgu 25):* ileride gelecek
    `op_disable_tenant_admins` yalnız üyelik durumuna dokunur, kimliğe asla.
13. **K-13 — §4.5 sınıflandırması.** **(b) — üçüncü açık istisna.** ⚠️ *Değişti (2026-10-21,
    denetimden sonra; önce (a) seçilmişti).* Gerekçe: yazan ve tenant sınırını geçen fonksiyonları
    md.7 çözümleyicisi saymak md.7'nin salt-okuma tanımını genişletiyor ve sınırı görünmez kılıyordu;
    kullanıcı istisnanın adıyla yazılmasını seçti. md.7'de yalnız salt okuma çözümleyiciler kalır.
    CLAUDE.md metni: ADR 0031 §6.1 (M14-01 uygular).
14. **K-14 — `admin_users.email` ayna, `full_name` üyelikte.** (a) seçildi. ⚠️ *Sıkılaştı (bulgu 21):*
    `tappa_app`'ın `UPDATE (email)`'i expand'de kapanır.
15. **K-15 — E-posta değiştirme (yeni, bulgu 12) — AÇIK, M14-01'de sorulur.** (a) M14'ün dışında;
    kendi kartı ve kararıyla gelir (mevcut parola + yeni adres doğrulaması + eski adrese bildirim +
    çakışmada aynı "gelen kutunu kontrol et" + aynanın bütün işletmelerde definer ile güncellenmesi).
    (b) M14'e ek kart. — **Öneri: (a).** ADR 0031 (a)'yı varsayar.

---

## P0 — Kararlar ve ölçüm

## M14-01 — Kural metinleri ve kalan soru

- **Bağımlılık:** kullanıcının *"başla"*sı
- **Kırmızı çizgi:** §4.5 (metin değişikliği), §6
- **Araç:** orkestratör
- **Boyut:** S
- **Commit:** `docs(rules): add the panel identity as the third tenant-isolation exception`

**Amaç.** ADR 0031 §6.1'deki CLAUDE.md metnini **birebir** uygulamak; ADR notlarını yazmak; K-15'i
sormak.

**Dokunulacak dosyalar.** `CLAUDE.md` §4.5 ("üç tanedir", (3) maddesi, "Bu üç istisnanın"), §6
(tenant-kapsamsız tablo listesi), §3 (`internal/adminauth` satırı) · ADR 0002 (md.7'ye not: giriş
çözümleyicisi ≤ 1'e döner; yazan kimlik fonksiyonları md.7 değil ADR 0031) · ADR 0013 (contract'ta
emekli olacaklar) · ADR 0015 (yetkinin nesnesi M14-08'den itibaren kimlik) · ADR 0025 (istisna listesi:
`/admin/switch`, "Sign out everywhere", "Link a business", e-posta doğrulama talebi —
`protectWritingThroughSuspension` grubu) · ADR 0028 / backlog (devre dışı üyeliğin API anahtarı;
oturumun mutlak ömrü) · `docs/backlog.md:294` ("Tenant değiştirici" → M14) · `open-questions.md`
(K-15 cevabı).

**Kabul kriterleri.** CLAUDE.md diff'i yalnız §6.1 metni; ADR 0031 durum satırı K-15 cevabıyla
güncel; `go test ./cmd/tappa/ -run 'EveryNamedTest' -count=1` yeşil.

## M14-02 — Canlı salt sayı ölçümü

- **Bağımlılık:** —
- **Kırmızı çizgi:** §7 (e-posta belgeye/log'a yazılmaz)
- **Araç:** yapıcı + kullanıcı (küme erişimi)
- **Boyut:** S
- **Commit:** `docs(plan): record the admin identity counts M14 starts from`

**Amaç.** `tappa_owner` ile salt okunur transaction'da yalnız sayılar: dolu e-postalı satır; `lower(email)`
grup boyutları (1, 2, 3+); e-postasız satır; `disabled` satır; canlı panel oturumu; **tek aktif owner'ı
bir çok-satırlı grupta olan tenant sayısı** (contract ön koşulunun başlangıç değeri). Sorgu metni
runbook'a, sayılar tarihle `state.md`'ye. Parolaların aynı olup olmadığı ölçülemez (tuzlu digest) —
öyle yazılır.

---

## P1 — Şema ve RLS

## M14-03 — Rol, kimlik tablosu, üyelik sütunları, tetikleyici

- **Bağımlılık:** M14-01
- **Kırmızı çizgi:** §4.5 (üçüncü istisna) · §6 · §4.6
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(db): add admin identities and link memberships to them`

**Dokunulacak dosyalar.** `scripts/db-init/01-roles.sql` (rol; migration'da `CREATE ROLE` yok) ·
`deploy/README.md` (canlı küme runbook: rol + GRANT, **migration'dan önce**) · **iki migration dosyası**
(N: tablo + sütunlar + tetikleyici; N+1: `UNIQUE (tenant_id, identity_id)` `CONCURRENTLY`, goose
no-transaction) · `scripts/pg-restore-verify.sh` manifesti · `internal/db/privcatalog_test.go`
(`privCatalogAllowed`, CLAUDE.md §6 / T137) · `internal/db/operatorschema_test.go:513-517`, `:556-570`
ve `internal/db/m11schema_test.go:94` rol listeleri.

**Kabul kriterleri.**
- `admin_identities` (`confirm_open_until` dahil, `status` yok): R5 muafiyet satırı (WARN'da görünür,
  FAIL yok), `ENABLE` + `FORCE`, `tappa_app` tablo + sıra açık `REVOKE ALL`, citext `UNIQUE` (harf
  farkıyla ikinci satır reddedilir), bcrypt CHECK, `tenant_id` adlı sütun yok.
- `admin_users.identity_id`, `linked_via` (`claim` değeri dahil — S7), `linked_at`, `unlinked_at`:
  `tappa_app` INSERT ve UPDATE `permission denied`; `tappa_app` `UPDATE (email)` `permission denied`;
  `ADD COLUMN` + FK kendi `lock_timeout`'uyla. `admin_identities`'e `confirm_opened_at`,
  `confirm_open_until`, `confirm_scope`. `CHECK (identity_id IS NULL OR linked_at IS NOT NULL)` (T12).
- **SELECT sütun düzeyine iner** (S9): `00006:115`'in tablo düzeyi SELECT'i `REVOKE` edilir ve `GRANT
  SELECT (id, tenant_id, full_name, email, password_hash, role, status, created_at, last_login_at,
  identity_id)` verilir; `tappa_app` `linked_via`, `linked_at`, `unlinked_at` için `permission denied`
  (R16); bugünkü bütün sorgular yeşil kalır (`admin_users` üzerinde `*` seçimi yok).
- **Tetikleyici** (R6): fonksiyon **SECURITY INVOKER** (katalogda `prosecdef = false` pinli),
  `EXECUTE` yalnız sahibinde (`REVOKE … FROM PUBLIC`, ADR 0028 §1, `00061`); `CREATE TRIGGER` kendi
  `lock_timeout`'uyla. Bağlı satırda `password_hash` / `email` değişikliği `tappa_app` olarak RAISE,
  `tappa_identitydefiner` olarak geçer; bağlanmamış satırda bugünkü yazmalar geçer. **İkinci kol**
  (S8): `OLD.unlinked_at IS NOT NULL` ve `password_hash` değişiyor ve definer değil → RAISE. Mutasyon:
  fonksiyon DEFINER yapılınca, ikinci kol silinince test kırmızı.
- `admin_sessions.authenticated_at` (R15): önce `NULL` sütun, `DEFAULT` ayrı ifadeyle; eski satırlar
  `NULL` kalır; `tappa_app` UPDATE edemez; `ALTER` kendi `lock_timeout`'uyla.
- `tappa_operator`, `tappa_opdefiner`: kimlik tablosunda `has_any_column_privilege = false`.
- Sahip kümesi ve rol öznitelik testleri `tappa_identitydefiner`'ı içerir (NOLOGIN, BYPASSRLS,
  NOSUPERUSER, üye 0, varsayılan ACL yok).
- **RLS izolasyonu (filtresiz):** A'nın bağlantısı aynı kimliğin B'deki satırını görmez; pozitif kontrol.
- **Eski binary yeni şemada:** bugünkü `CreateAdminUser`, `SetOwnAdminPassword` (bağlanmamış satır),
  `TouchAdminSession` değişmeden çalışır.
- Down: Up → Down → Up temiz. Mutasyon: sütun GRANT'ı açılınca, tetikleyici kaldırılınca kırmızı.

**Tuzaklar.** `ALTER DEFAULT PRIVILEGES` yeni tabloya yetki verir — açık REVOKE şart. `admin_users`
sıcak tablodur. Rolü migration'da yaratmak R5b'yi kırar.

## M14-04 — Çözümleyiciler ve oturum fonksiyonları

- **Bağımlılık:** M14-03
- **Kırmızı çizgi:** §4.5 (üçüncü istisna) · §4.6
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(db): resolve identities and switch a panel session between memberships`

**Amaç.** ADR 0031 §5.2: `resolve_identity_by_email`; `identity_open_session`,
`identity_memberships`, `identity_switch_session`, `identity_revoke_sessions`; elle yazılmış Go
çağıranları (`internal/db/resolve.go` emsali); `TouchAdminSession`'a `tenants.name` ve `identity_id`.

**Kabul kriterleri.**
- **İmza katalog testi** (ADR 0027 §2 yöntemi: `pg_get_function_identity_arguments` +
  `pg_get_function_result` birebir) ve **fiil × sütun matrisi testi**; `tappa_identitydefiner`'ın
  sahip olduğu fonksiyon kümesi tam olarak ADR'deki liste.
- `has_function_privilege('tappa_operator', <her identity_* / resolve_identity_*>, 'EXECUTE') = false`.
- Çözümleyici ≤ 1 satır, `OPERATOR(public.=)`, sahibi `tappa_resolver`, yazmaz; `has_active_membership`
  döndürür (R14).
- Geçiş: üyeliği olmayan / devre dışı hedef / iptal edilmiş oturum / başka kimliğin oturumu /
  bağlanmamış satırın oturumu → 0 satır (ayrı testler); başarıda yeni oturum, eski iptal,
  `authenticated_at` kopyası, `last_used_tenant`; **iki audit satırı sıradan giriş/çıkış satırlarıyla
  bayt-aynı** (tarama: karşı tenant id/adı, üyelik sayısı, `identity_id`, `via` yok).
- Eşzamanlılık: N paralel geçiş → tam bir yeni canlı oturum (`-race`).
- `identity_memberships` başka kimliğin üyeliğini hiçbir girdiyle döndürmez; LIMIT 50.
- `identity_id`'yi `TouchAdminSession` dışında hiçbir sqlc sorgusu seçmez ve `admin_users` üzerinde `*`
  / `a.*` seçimi yoktur (sorgu-metni taraması, R16); `linked_via`'yı hiçbir sqlc sorgusu seçmez.
- BYPASSRLS gövdelerinde tenant tablosuna dokunan her ifadede açık `tenant_id` yüklemi (metin taraması).
- Mutasyon: kimlik eşitliği, `status='active'`, eski oturum iptali silinince; audit'e hedef eklenince
  → kırmızı.

## M14-05 — Parola, sıfırlama, bağlama, çözme, doğrulama, olay fonksiyonları

- **Bağımlılık:** M14-03
- **Kırmızı çizgi:** §4.5 · §4.7 · §4.6
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** XL
- **Commit:** `feat(db): set identity passwords, reset them by token and link legacy rows`

**Amaç.** `admin_identity_resets`, `admin_identity_verifications`, `admin_identity_events` (M14-03'ün
tenant-kapsamsız kuralları) + ADR 0031 §5.2'deki `identity_password_for_session`,
`identity_set_password`, `identity_consume_row_reset`, `resolve_identity_reset_by_token_hash`,
`identity_consume_reset`, `identity_link_legacy`, `identity_unlink_backfilled`,
`identity_relink_refused`, `identity_request_verification`,
`resolve_identity_verification_by_token_hash`, `identity_verify_email`, `identity_record_failed_login`,
`identity_sweep_events`; `resolve_admin_by_email` `unlinked_at IS NULL` süzgeciyle ve dönüşüne
`identity_id` eklenerek yeniden tanımlanır (S8, T2 — orkestratör kararı (a); dönüş tipi değiştiği için
`DROP` + `CREATE`; md.7 (iii) genişlemesi ADR 0031 §5.2'de gerekçeli; eski binary sütunları adıyla seçer,
`internal/db/resolve.go:301-302`). `tappa_resolver`'a `admin_users (identity_id, unlinked_at)` SELECT
(T6).

**Kabul kriterleri.**
- **Yeniden tanım** (T5): `resolve_admin_by_email`'in yeni tanımı `SECURITY DEFINER`, `SET search_path`,
  sahibi, `REVOKE ALL FROM PUBLIC` ve `GRANT EXECUTE TO tappa_app`'i açıkça yeniden yazar; katalog testi
  `prosecdef`, `proconfig`, `tappa_app` EXECUTE ve `tappa_resolver`'ın sütun yetkilerini pinler; eski
  binary'nin sorgusu (`SELECT id, tenant_id, password_hash, status FROM resolve_admin_by_email(…)`) yeni
  tanımda çalışır.
- **Kimliği yazan her fonksiyon kimliği bir sırdan bulur** (üç adlandırılmış istisna hariç);
  e-postayla çözen bir dual-write yolu yok (mutasyon: e-postayla çözülürse kırmızı). BYPASSRLS
  gövdelerinde tenant tablosuna dokunan her ifadede açık `tenant_id` yüklemi (metin taraması).
- `identity_set_password`: bağlı oturumda kimlik + bağlı bütün satırlar, öbür oturumlar bütün
  işletmelerde iptal, mevcut korunur; **bağlanmamış oturumda 0 satır**.
- `identity_consume_row_reset` / `identity_consume_reset`: tek kullanımlık, oturum vermez, eski
  satırları **bağlamaz**, `email_verified_at`'i doldurur (satır-başı link dahil — S5), **her** tüketimde
  `reset` kapsamlı onay penceresi açılır (R2, S3).
- `identity_link_legacy` (R3): iki canlı oturum şart (biri bağlı, biri bağlanmamış ve `unlinked_at IS
  NULL`) **ve ikisinin de `authenticated_at` son 5 dakika içinde**; `NULL` → 0; tek oturum, iki bağlı,
  iki bağlanmamış, e-posta farkı, bayat oturum → 0; iki yön test edilir. Mutasyon: tazelik yüklemi
  silinince kırmızı.
- `identity_unlink_backfilled` (R1, S3, S4): **taze** oturum (`authenticated_at` son 5 dk), `email_verified_at
  IS NOT NULL`, `now() < confirm_open_until`; listelenebilir küme pencerenin kapsamına göre —
  `first_verify` → `linked_via ∈ {backfill, reconcile, signup}`; `reset` → `linked_at < confirm_opened_at`
  olan **bütün** bağlar, `claim` ve `invite` **dahil** (T1); contract sonrası doğmuş satırda "çöz" =
  `status='disabled'`. Çözmede satırın süresi dolmamış `password_resets` linkleri `cancelled_at` ile iptal
  (T5; test: çözülen satırın eski linki tüketilince 500 değil tek tip ret). Satıra **nöbetçi digest** — 32 rastgele baytın bcrypt'i,
  baytlar atılır, repoda sabit yok (S20) — + `unlinked_at`; o tenant'ın oturumları iptal. Testler: çözme →
  aynı parolayla giriş → bağlama yok, satır açılmaz (kimlik yolu **ve** yeniden tanımlanmış eski
  çözümleyici); çözülen digest ≠ kimlik digest'i; pencere kapalıyken 0; bayat oturumla 0; **önceden
  ekilmiş kimlik + `password` bağı + kurbanın sıfırlaması → bağ listelenir** (S3); **ekilmiş kimlik +
  iletilmiş linkle `claim` + kurbanın sıfırlaması → listelenir** (T1).
- `identity_relink_refused` (S4, T4): yüklem `tenant_id = p_tenant`, `email OPERATOR(public.=)
  identity.email`, `unlinked_at >= confirm_opened_at` (yalnız bu pencerede reddedilmiş), pencere açık;
  taze oturum + parola yeniden girişi; o tenant'ta kimliğin başka üyeliği varsa (`UNIQUE (tenant_id,
  identity_id)`) 0; özgün `linked_at` korunur → satır pencerenin geri kalanında listede kalır. Pencere
  kapanınca 0 (üyelik ancak yeni bir davetle döner).
- Tetikleyicinin ikinci kolu (M14-03) ile birlikte: reddedilmiş satırın digest'ini `tappa_app` ya da
  `tappa_owner` sıfırlama tüketimiyle değiştiremez (S8).
- `identity_verify_email` (R2, S16): tıklayan tarayıcıda token'ı isteyen **kimliğin** canlı bir oturumu
  yoksa 0 satır; istek ile tıklama arasında işletme değiştiren (yeni oturum) kişi doğrulanır.
- `identity_record_failed_login` (R9, R17): yalnız `admin_identity_events`, `identity_id NOT NULL`,
  `detail`'de e-posta ve IP yok; bilinmeyen e-postada çağrılmaz. `identity_sweep_events` 90 günü geçen
  satırların tek silme yolu (`00058` deseni).
- Değişmez testi: bağlı her satırda `password_hash = kimliğin digest'i` her fonksiyondan sonra
  (çözülen satır hariç — artık bağlı değil).
- İmza ve matris testleri M14-04'ünkine eklenir.

## M14-06 — Backfill

- **Bağımlılık:** M14-02, M14-03
- **Kırmızı çizgi:** §4.5 · §6 · `deploy/README.md` (`lock_timeout`)
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(db): backfill admin identities, oldest active account first`

**Kabul kriterleri.**
- Başta: koşan rol superuser ya da BYPASSRLS değilse migration **hata** verir (FORCE RLS altında 0
  satırı sessizce görmesin). Sonda: **e-postalı satır varsa** bağlı satır sayısı > 0 değilse hata —
  boş veritabanında (CI, testcontainers, yeni geliştirme veritabanı; migration seed'den önce koşar)
  koşulsuz geçer (R11). İki durum da testli (boş veritabanı + dolu fikstür).
- Çok satırlı grupta **en eski aktif** satır (yoksa en eski) kimliği doğurur; öbürleri bağlanmamış.
- İdempotent; `linked_via='backfill'`; Down yalnız bunları çözer ve yalnız bu migration'ın kimliklerini
  siler.
- Fikstür: KF/KM owner'ları; çift e-posta; daha **sonra** kurbanın e-postasını taşıyan saldırgan satırı
  bağlanmaz; daha **önce** taşıyan ama `disabled` saldırgan satırı da kimliği doğurmaz.
- Süre tahmini M14-02 sayısıyla yazılı.

## M14-06b — Uzlaştırma

- **Bağımlılık:** M14-06
- **Kırmızı çizgi:** §4.5
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** S–M
- **Commit:** `feat(db): reconcile admin identities born after the backfill`

**Amaç.** Backfill'den sonra doğan satırlar (eski pod penceresi; expand ile M14-17 arası kayıtlar)
için aynı kuralla kimlik/bağ (`linked_via='reconcile'`, `linked_at` dolu); `unlinked_at` damgalı satırları
**atlar**. Bir **bakım fonksiyonudur** (T3): `identity_reconcile()`, sahibi `tappa_identitydefiner`,
`EXECUTE` yalnız `tappa_owner`'da; migration onu yaratır ve çağırır, runbook M14-19'dan önce yeniden
çağırır (idempotent), contract B düşürür. Uygulama canlıyken koştuğu için kimlik INSERT'i ad alanlı e-posta
kilidini ve `ON CONFLICT DO NOTHING`'i kullanır (T8).

**Kabul kriterleri.** İki koşu aynı sonuç; backfill'in testleri (boş veritabanı dahil) uzlaştırma için
de yeşil; reddedilmiş satır hiçbir koşuda bağlanmaz; uzlaştırma ile eşzamanlı bir kayıt → 500 yok
(T8); `tappa_app` fonksiyonu çağıramaz; M14-19'un iki ön-kontrol sayımı (kimliği olmayan
dolu e-posta = 0, reddedilmişler hariç; bağlı satırda digest uyuşmazlığı = 0) bu kartın runbook adımıdır.

---

## P2 — Domain / auth

## M14-07 — Kimlik doğrulaması ve otomatik bağlama

- **Bağımlılık:** M14-04, M14-05
- **Kırmızı çizgi:** §4.5 (00011 YÜKÜMLÜLÜK 1, 2, 5) · §7
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): authenticate a person by identity and link proven legacy rows`

**Kabul kriterleri.**
- **Her giriş tam 1 + `MaxCandidates` karşılaştırma** öder; zamanlama testi **beş** durumda düz
  (R14): kimlik var/yok × bağlanmamış satır var/yok (bilinmeyen e-posta dahil) + doğru parola ama bütün
  üyelikleri devre dışı (`has_active_membership = false` → sahte çağrı). Bugünkü
  `TestPanelE2E_TimingIsFlatOverHTTP` deseni. Ölçü **süre**, gidiş-dönüş sayısı değil (S15): sahte çağrı
  yazan `identity_open_session` kadar sürer; bcrypt `has_active_membership`'e göre asla atlanmaz ya da
  öne çekilmez (dal karşılaştırmadan sonra); bilinen e-postada olay satırının yazılması testin içinde
  (`failLogin` deseni, `adminlogin.go:1288-1333`).
- Bilinmeyen / yanlış / devre dışı üyelik tek hata değerine çöker.
- Eski kol kimliğin **kendi** satırlarını (`identity_id` eşit) adaylardan çıkarır (T2); test: 8 bağlı + 1
  bağlanmamış satırı olan kişi doğru parolayla girer → bağlanmamış satır otomatik bağlanır.
- Aynı parola kimliği ve eski satırı doğrularsa: eski satırda **taze** oturum → kimlikte taze oturum →
  `identity_link_legacy(iki hash)` → eski oturum iptal; `linked_via='password'`; `unlinked_at` damgalı
  satırlar aday bile değildir.
- `TestAuthenticate_RefusesTheCrossTenantBypass`'ın senaryosu kimlik yolunda: saldırganın satırı
  kurbanın kimliğine bağlanmaz ve oturum açmaz.
- `identity_open_session`'ın tek çağrı yolu doğrulanmış-kimlik tipidir; ikinci bir üretici varsa
  adıyla yazılır (`manager.go:251-292`'nin iki-bacak dürüstlüğü).
- Başarısız kimlik girişi `identity_record_failed_login` ile, kimlik başına kova (aşınca tek
  `FirstOverLimit` satırı); log'da e-posta yok.

## M14-08 — Oturum işlemleri, bayrak, dual-write

- **Bağımlılık:** M14-07
- **Kırmızı çizgi:** §4.5 · §4.7 · ADR 0015
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): open, switch and revoke panel sessions through the identity`

**Amaç.** `Open`, `Switch`, `RevokeAll`, kimlik üstünden `ChangeOwnPassword`; bayrak
`TAPPA_ADMIN_IDENTITY` — `deploy/k8s/05-config.yaml`'da `off`, `internal/config` doğrular, paketleme
testleri (`TestPackaging_NoConfigMapKeyIsAlsoAnEnvEntry` ve ConfigMap anahtar listesi) güncellenir; ADR
0031 §8'in **"bayrağın kapattığı yüzeyler" tablosu** tek bir yerde (ör. bir kapı yardımcısı)
uygulanır ve sonraki kartlar ona bağlanır. **Yazma yolları bayraktan bağımsız**: kayıt
`identity_create_for_signup`'ı çağırır (ADR 0031 §5.2'nin yapısal yüklemleri: aynı transaction'da
yazılmış tek satır, `app.tenant_id` eşitliği, repodaki ad alanlı kilit deseniyle
`hashtextextended('admin-identity-email:' || lower(email), 0)` + `ON CONFLICT DO NOTHING` — S13);
bağlı satırın parola değişimi `identity_set_password`, bağlanmamışınki bugünkü tek-satır yolu; eski
sıfırlama linki bağlı satırdaysa `identity_consume_row_reset` — **ADR 0015'in anlam değişikliği burada
başlar**.

**Kabul kriterleri.**
- Bayrak `off`: giriş/seçici davranışı bayt-aynı (mevcut testler değişmeden yeşil); **bağlı
  tek-üyelikli oturum ile bağlanmamış oturum kabuk, hesap sayfası ve `/admin/switch` (404) için
  bayt-aynı** yanıt alır (R4; işletme adı ve oturuma özgü token'lar dışında).
- Var olan e-postayla kayıt: kimlik digest'i bayt-aynı, yeni satırın `identity_id` NULL; aynı e-postayla
  **eşzamanlı** iki kayıt → 500 yok, tek kimlik (R13).
- **Gerçek `Provision` yolundan** yeni e-postayla kayıt → kimlik doğar (S13: `xmin` yüklemi bir SAVEPOINT
  altında sessizce tutmazsa bu test kırmızı); `pg_current_xact_id()::xid` 17.10'da doğrulanır.
- `on`'da parola değiştir → `off`'ta yeni parolayla gir (dual-write) ve tersi.
- `Switch` yeni token üretir; eski token çözülmez; `Verify` yolu değişmemiş (diff).
- Davet/kayıtla doğan bağlı satırın digest'i kimliğinkine eşit (değişmez testi).

---

## P3 — Giriş, geçiş, oturum

## M14-09 — Giriş handler'ı

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5 · §7
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): sign a person in to their last business`

**Kabul kriterleri.**
- Bayrak `on`: tek üyelik → doğrudan; çok üyelik → `last_used_tenant`, o üyelik devre dışıysa en
  eski aktif üyelik. Eski kolda birden çok satır doğrularsa **seçici** (contract'a dek).
- Kimlik yolunda tenant audit satırlarından kişinin öbür işletmeleri hakkında bilgi taşıyan her alan
  kalkar (R21): `VerifiedBusiness` (`adminlogin.go:1125`, `:1204`), `ActionAdminLoginTruncated`'in
  `Resolved` / `Compared`'ı (`:1555-1562`), ret satırının `AttemptedTenant`'ı (`:1203`, `:1547-1549`);
  audit metin taraması bunları ve `identity_id`'yi yasaklar. Sayılar yalnız `admin_identity_events`'te.
- `adminLoginWorkLimit` 1 + 8 karşılaştırmaya göre yeniden boyutlanır (ölçüm rapora);
  `adminAccountLimit` kimlik yolunda `identity_id`'ye anahtarlı.
- Başarısızlık gövdesi senkronizör token dışında bayt-aynı; bayrak `off` yolunda
  `TestAdminLogin_OneVerifiedBusinessSkipsThePicker`, `TestAdminChoose_RefusesATenantOutsideTheSignedSet`
  değişmeden yeşil.

## M14-10a — Panel formlarının tenant bağı

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(panel): bind every panel form to the business it was rendered for`

**Amaç.** ADR 0031 §5.5: her panel POST formunun `action` URL'si render anındaki tenant'ın bağını
**sorgu parametresi** olarak taşır (gövde değil — R7); değer `HMAC(K, "tappa/admin-form-tenant/v1|" +
tenant_id)`, `K` **ham oturum token'ından** türetilir (`token_hash`'ten asla); `ProtectWriting` ve
`protectWritingThroughSuspension` gövdeye dokunmadan, yazmadan önce karşılaştırır; uyuşmazlık 409
*"This page belongs to another business — reload it."*

**Kabul kriterleri.**
- Bütün panel POST formlarının envanteri (şablon taraması) ve her `action`'da bağ; bağsız bir panel
  POST formu bir tarama testinde kırmızı.
- A'da render edilen form, B'ye geçtikten sonra gönderilir → B'de **0 satır** ve 409 (uçtan uca).
- Bağ uyuşmazsa gövde **ayrıştırılmaz**: handler'ın `MaxBytesReader` sınırından büyük bir gövdeyle
  gönderilen uyuşmaz istek, gövde okunmadan 409 alır (`billingactions.go:281-296`,
  `brandactions.go:339`, `:416`, `accountactions.go:109`, `brandupload.go:42`, `:321` sınırları korunur).
- Bağ başka bir oturumun token'ıyla üretilemez (senkronizör özelliği testi); anahtar rotasyonunda açık
  formların 409'u adlandırılmış bedel olarak runbook'ta.
- Bağ bağlam kurmaz (`AdminOf` dışında tenant kaynağı yok testi). Bayrak `off` iken de açık.
- Bağ hiçbir yanıt URL'sinde kalmaz (S14): her bağlı POST **POST-redirect-GET** ile biter, yönlendirme
  hedefinde `tb` yok (test); uygulama log'u yalnız rota desenini yazar (`requestlog.go:436-452`), panel
  `no-referrer` gönderir (`adminlogin.go:1763-1768`); runbook ingress erişim log'unun kapalı ya da
  sorgusuz olduğunu doğrular.

## M14-10 — Geçiş rotaları

- **Bağımlılık:** M14-10a
- **Kırmızı çizgi:** §4.5 · ADR 0025
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(panel): switch businesses without signing out`

**Kabul kriterleri.**
- `POST /admin/switch` zinciri `protectWritingThroughSuspension` + senkronizör
  (`HMAC(…, "tappa/admin-switch/v1|" + session_id)`, sabit-zamanlı); çapraz kökenli ve token'sız istek
  reddedilir (`TestAdminAuth_RefusesForgedAndCrossSiteRequests` deseni).
- Oran kovası `identity_id` (+ adres): A/B arasında N alternatif geçiş → limit+1'de 429 ve audit satır
  sayısı artmayı keser.
- Hedef reddi tek tip; eşzamanlı kaybedenler `Set-Cookie` yazmaz ve çerezi silmez.
- ADR 0025'in isimli istisna sayısını sayan test aynı commit'te güncellenir (iddia gevşetilmez).
- Askıdaki hedefe geçiş başarılı; sonraki yazma askı kapısına takılır.
- Rota `/admin` altında (`TestAdminRoutes_AreAllUnderTheCookiePath`); bayrak `off` iken iki rota 404.

## M14-11 — Her yerden çık

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** S–M
- **Commit:** `feat(account): sign out of every business at once`

**Kabul kriterleri.** "Sign out everywhere" bayrakla kapılı ve `protectWritingThroughSuspension`
grubunda; kişinin bütün
işletmelerindeki oturumlar iptal, her tenant'a sıradan çıkış satırı; parola değişimi öbür oturumları
bütün işletmelerde iptal eder (bugün tek tenant — `changepassword.go:110`).

---

## P4 — Panel arayüzü

## M14-12 — Kabukta işletme adı ve değiştirici bağlantısı

- **Bağımlılık:** M14-10
- **Kırmızı çizgi:** §9
- **Araç:** yapıcı, skill `tappa-brand`
- **Boyut:** M
- **Commit:** `feat(panel): show the current business and a switch link in the shell`

**Kabul kriterleri.**
- İşletme adı her tenant'ta (bugün yalnız markalıda — `admin.templ:456-460`, `panelbrand.go:108-116`);
  kaynak `TouchAdminSession`, sayfa başına ek okuma 0.
- "Switch business" bağlantısı bayrakla kapılı; bayrak `on` iken: her zaman görünür (tek üyelikte sayfa
  "tek işletmen var" der) ya da
  üyelik sayısı oturum açılışında/geçişte damgalanır — yapıcı ikisini ölçer, seçimi rapora yazar; ek
  okuma 0 kalır.
- Üç genişlik, ≥ 44 px, tek `<h1>`; `TestUnbrandedScreens_AreByteIdenticalToTheGolden` ve marka CSS
  golden'ı bayt-aynı; accent slotları büyümez.

## M14-13 — Değiştirici sayfası ve Choose metni

- **Bağımlılık:** M14-10
- **Kırmızı çizgi:** §9
- **Araç:** yapıcı, skill `tappa-brand`
- **Boyut:** S–M
- **Commit:** `feat(panel): list a person's businesses on the switch page`

**Kabul kriterleri.** `/admin/switch` Choose panolarının adisyon düzeniyle (ad, rol, "Continue to
{name}", askıda K-10 işareti, her satırda tenant bağlı form); Choose ekranı (eski kol) çıkmadan geçişi
anlatır; üç genişlik; bayrak `off` iken sayfa 404.

---

## P5 — Sıfırlama, davet, kayıt, hesap

## M14-14 — Kimlik başına sıfırlama

- **Bağımlılık:** M14-05, M14-08
- **Kırmızı çizgi:** §4.7 · §7 · ADR 0015 · ADR 0022
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): reset a person's password once for every business`

**Kabul kriterleri.** Kimlik başına talep bayrakla kapılı (`off` iken bugünkü satır-başı talep).
Adres → kimlik → en çok bir canlı link (bugün en çok 8); kayıtlı/kayıtsız adres yanıt ve zamanlamada
ayırt edilemez (EM-5 asenkron deseni); link GET'te tüketilmez, tüketim POST (bugünkü `readLink` deseni,
`adminreset.go:1415-1425`); tüketim kimliği + bağlı satırları yazar, bütün oturumları iptal eder, oturum
vermez, eski satırları bağlamaz ve **her** tüketimde onay penceresini açar (R2; liste M14-18); bağlanmamış satırlar contract'a dek satır başı link alır; şablon "kimlik başına
sıfırlama" ve kişi başına bir "parolanız değişti" (EM-9).

## M14-15 — Yönetici daveti

- **Bağımlılık:** M14-08, M14-14
- **Kırmızı çizgi:** §4.5 · §4.6 · §4.7 · ADR 0005 Y-D'nin yönetici ikizi
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** XL
- **Commit:** `feat(auth): invite a manager by e-mail`

**Amaç.** M7-07'nin (`m7-portal.md:1509-1591`) bütün kriterleri, kimlik modelinde: tenant-kapsamlı
`admin_invites` (RLS beşlisi; owner listeler/iptal eder), `resolve_admin_invite_by_token_hash`,
`identity_accept_invite` / `identity_accept_invite_new`. Yüzey **bayrakla kapılı** (R12; bayrak
`off` iken 404).

**Kabul kriterleri.** M7-07'nin altı maddesi · kabul edilmeden `admin_users` satırı 0 · iletilmiş link
(kimliğin e-postası ≠ davet e-postası) → 0 · link GET'te tüketilmez, kabul POST (R19) · var olan
kimlikle kabul: aynı e-postalı kimliğin canlı oturumuyla POST posta kutusu kanıtıdır →
`email_verified_at` boşsa dolar, onay penceresi açılır; önce sıfırlama **dayatılmaz** (orkestratör
kararı) · kabul oturum vermez · **davet eden owner adresin kimliği olup olmadığını öğrenmez** — davet
yanıtı, zamanlaması ve davet listesindeki durumu iki durumda aynı (R18) · holding üyeliği davet doğurmaz
· baseline materyalize edilmiş tenant'ta owner kilitlenmez (önce ölç) · oranlar EM-7C'nin
`(tenant_id, mailbox)` kovasıyla · şablon ADR 0022 §3/§4 · `identity_accept_invite_new`'in kimlik
INSERT'i aynı e-posta kilidi + `ON CONFLICT DO NOTHING`; eşzamanlı davet kabulü + kayıt onayı → 500 yok
(S13).

## M14-16 — Üyelik yönetimi

- **Bağımlılık:** M14-15
- **Kırmızı çizgi:** §4.5 · §4.6
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** M–L
- **Commit:** `feat(panel): disable a manager and transfer ownership`

**Kabul kriterleri.** Yüzey bayrakla kapılı. Owner o işletmedeki üyeliği `disabled` yapar → yalnız o tenant'ın oturumları
düşer (iki tenant'lı test); son aktif owner düşürülemez/indirilemez (eşzamanlı iki indirme testi);
devir iki adım, her adım audit'te; kimliği etkileyen hiçbir yol yok.

## M14-17 — Kayıt: e-posta doğrulaması ve var olan adres

- **Bağımlılık:** M14-08, M14-14
- **Kırmızı çizgi:** §4.5 · §7 · ADR 0013 · ADR 0022
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** XL
- **Commit:** `feat(signup): verify the owner's e-mail before the business exists`

**Amaç.** ADR 0031 §5.10: `admin_signup_claims` (tenant-kapsamsız, TTL), `identity_open_signup_claim`,
`resolve_signup_claim_by_token_hash`, `identity_claim_signup`, `identity_sweep_signup_claims`. Kayıt
`identity_create_for_signup`'ı **çağırmayı bırakır**; fonksiyon **düşürülmez** — DROP contract B'de
(R8: bu deploy'un rollout penceresindeki eski pod onu çağırabilir).

**Kabul kriterleri.**
- Tıklamadan önce kimlik/tenant/üyelik 0 satır; yeni ve var olan adres için yanıt ve zamanlama ayırt
  edilemez.
- Link GET'te tüketilmez; onay POST (R19).
- **Yeni adres:** onay sayfası formda seçilen parolayı yeniden ister; yanlış parola → hiçbir şey doğmaz
  (R2 — başka bir kişinin tıklaması saldırganın parolasıyla kimlik doğuramaz). Karşılaştırma
  **uygulamadadır** (S12): çözümleyici talebin digest'ini yalnız talep sırrıyla döndürür, uygulama
  fonksiyonu yalnız eşleşmede çağırır — Go tipiyle bağlı güvence, `identity_open_session` gibi
  adlandırılır. Tenant'ı uygulama yaratır (`WithTenant`), definer kimliği + owner satırını yazar
  (`linked_via='claim'`, S7).
- **`identity_claim_signup`'ın yapısal yüklemleri** (S2, T7): tenant'ın `admin_users` satırı yok (asıl
  güvence), `tenants.created_at = now()` (transaction başlangıcı; alt transaction'dan etkilenmez),
  ek olarak `tenants.xmin = pg_current_xact_id()::xid` (xmin "bu transaction'da yazılmış" demektir —
  SAVEPOINT altında yanlış; kayıt üst düzey `WithTenant` kullanır, `signup.go:689`),
  `p_tenant_id = current_setting('app.tenant_id')`, VAT ve ad taslağa eşit — biri tutmazsa 0 satır. Mutasyon: **var olan** bir tenant'ın id'siyle çağrı → 0
  satır (test). Definer matrisine `tenants (vat_number)` ve `xmin` okuması eklenir.
- `claim` satırları `first_verify` listesine girmez, `reset` listesine girer (S7, T1); contract'tan
  sonra doğan bir `claim` satırı için "çöz" = üyeliği devre dışı bırakmak (CHECK'e dokunmaz).
- Kimlik INSERT'i e-posta kilidi + `ON CONFLICT DO NOTHING` (S13).
- **Var olan kimlik:** yalnız kendi canlı oturumuyla sahiplenir; `identity.email = claim.email` değilse
  0 (R18); tıklama posta kutusu kanıtıdır → `email_verified_at` boşsa dolar, onay penceresi açılır
  (önce sıfırlama dayatılmaz).
- Süresi dolan talep hiçbir şey ayırmaz; iki bekleyen talep aynı VAT'ta → ikincisi bugünkü VAT çakışma
  mesajı; süpürme yalnız `identity_sweep_signup_claims` ile.
- Oranlar: `signupCreateLimit` + alıcı başına üst sınır; şablonlar "kayıt doğrulaması" ve "var olan
  adresle kayıt".
- `signInBlocked` ve `signup.sign_in_unreachable` kimlik yolunda emekli.
- Bu kart **M14-19'un ön koşuludur** (ADR 0031 §5.10).

## M14-18 — Hesap: işletmelerim, doğrulama, onay, bağlama

- **Bağımlılık:** M14-05, M14-07, M14-12
- **Kırmızı çizgi:** §4.5 · §7 · ADR 0025
- **Araç:** yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(account): list my businesses, verify my e-mail and link a business`

**Kabul kriterleri.**
- Bütün yüzeyler bayrakla kapılı (ADR 0031 §8 tablosu).
- "Your businesses" `identity_memberships`'ten.
- "Verify your e-mail": `identity_request_verification` token'ı **isteyen kimliğe** bağlar; link GET'te
  tüketilmez, onay POST ve tıklayan tarayıcıda **aynı kimliğin** canlı oturumunu ister (R2, R19, S16) —
  başka bir tarayıcıdan tıklama hiçbir şeyi doğrulamaz; arada işletme değiştiren kişi doğrulanır.
- Onay penceresi: `first_verify` kapsamında `linked_via ∈ {backfill, reconcile, signup}`; `reset`
  kapsamında pencere açılmadan önce kurulmuş **bütün** bağlar, `claim` ve `invite` dahil (S3, T1).
  Varsayılan tek tıklık **"Keep all"** (T11). Çözme kimlik
  parolasını **yeniden sorar** ve taze oturum açar; satır tenant'ın son aktif owner'ıysa açık uyarı;
  pencere açıkken "Undo" (`identity_relink_refused`; parola yeniden sorulur, geri bağlanan satır pencere
  sonuna dek listede kalır — S4, T4); pencere kapanınca liste ve geri alma kapanır.
- "Link a business": form **mevcut oturumun parolasını da** ister; o istekte iki tarafta taze oturum →
  eski parolayla en çok 8 aday (dolgulu) → `identity_link_legacy` → karşı geçici oturum iptal; ters yön
  de; reddedilmiş satır aday değil; sonuç tek tip; kimlik başına oran sınırı. Test: 6 dakikalık oturum +
  parola yeniden girişi → bağlama başarılı (S6).
- Bütün rotalar `protectWritingThroughSuspension` grubunda ve tenant bağlı (M14-10a).

---

## P6 — Bayrak ve contract

## M14-19 — Bayrağı açma

- **Bağımlılık:** M14-06b (canlıda ve yeniden koşturulmuş), M14-09 … M14-13, **M14-17** canlıda;
  M14-14 ve M14-18 canlıda (K-3 saatinin bayrakla başlaması için)
- **Kırmızı çizgi:** §7 · §4.5 (tek REVOKE)
- **Araç:** yapıcı + agent `tappa-db-migrator` (tek migration) + kullanıcı
- **Boyut:** S–M
- **Commit:** `feat(deploy): turn the admin identity on`

**Kabul kriterleri.**
- Ön kontroller (runbook, yalnız sayı): kimliği olmayan dolu e-posta = 0 (reddedilmişler hariç); bağlı
  satırda digest uyuşmazlığı = 0; **bağlı ama `unlinked_at` dolu satır = 0** (S19); uzlaştırma yeniden
  koşturuldu.
- **Tek migration, iki iş** (S5, S18, T9 — deterministik ve testli, elle UPDATE değil):
  (1) bayrak kapalıyken açılmış bütün onay pencerelerinin **yalnız** `confirm_open_until`'i `now() + 7
  gün` olur, `confirm_opened_at` **korunur** (reset listesinin kümesi ona bağlıdır); (2) `REVOKE EXECUTE ON
  FUNCTION identity_create_for_signup FROM tappa_app` — M14-17'den sonra hiç çağrılmaz; DROP contract B'de
  kalır. Eski pod (M14-17 sürümü) onu çağırmaz.
- Açma = `05-config.yaml`'da `on` commit'i + `main` deploy'u (kullanıcı kararı); `kubectl edit`
  kullanılmaz (sonraki deploy geri çevirir).
- Duman: KF ↔ KM geçişi, eski sekme 409, sign out everywhere, yanlış parola, davet.
- Geri dönüş (`off` commit + deploy) yazılı ve bir kez **denenmiş**.
- **K-3 saati bu deploy'un günüdür** (`max(bayrak, M14-14 + M14-18)`; ikisi zaten canlıda) ve
  state.md'ye tarihiyle yazılır.
- İzleme: e-postasız SigNoz sayaçları (kimlik yolu / eski kol girişleri, geçiş reddi, bağlama) ve
  haftalık "bağlanmamış satır", "tek owner'ı bağlanmamış tenant" ve ayrı olarak "reddedilmiş satırların
  tenant'ları" sayımı `state.md`'ye.

## M14-20a — Contract A (kod)

- **Bağımlılık:** M14-19 + K-3 (bayraktan itibaren 60 gün) + *"tek aktif owner'ı bağlanmamış tenant =
  0"* (reddedilmiş satırların tenant'ları ayrı raporlanır, kullanıcı karar verir) + kullanıcı onayı
- **Kırmızı çizgi:** §4.5
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `refactor(auth): stop calling the per-business sign-in`

**Amaç.** Binary eski kolu çağırmayı bırakır: `resolve_admin_by_email` çağrıları, `MaxCandidates`,
dolgu, `PickerCap`, imzalı seçici seti, `signInBlocked`, satır başı sıfırlama; bayrak ve kapı yardımcısı
kaldırılır (yüzeyler kalıcı açık). **Şema değişmez** (bu deploy'un rollout penceresindeki eski pod
çalışmaya devam eder).

**Kabul kriterleri.** Emekli mekanizmaların testleri silinmez, **dönüştürülür** (ör. "e-posta ≤ 1
kimlik"); `TestMaxCandidates_IsTheMeasuredCPUBound` ve seçici testleri yeni iddialarına aynı commit'te
yeniden adlandırılır ya da kaldırılır, `TestEveryNamedTestExists` yeşil; `manager.go:285-292` yorumu
tarihsel olarak işaretlenir; **eski binary (M14-19 sürümü) bu binary'nin şemasında** çalışır (şema aynı).

## M14-20b — Contract B (şema)

- **Bağımlılık:** M14-20a canlıda
- **Kırmızı çizgi:** §4.5 · §6
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(db): retire the per-business sign-in schema`

**Kabul kriterleri** (ADR 0031 §8 adım 8'in sırasıyla).
1. Bağlı satır tetikleyicisi düşer ve yerine contract sonrası kuralı gelir (bağlı satırda
   `password_hash` yalnız `NULL`, `email` yalnız `tappa_identitydefiner`) — "herkes NULL yazabilir"
   diye **gevşetilmez** (R6); "bağlı her satırda digest eşitliği" değişmezi ve testi bu commit'te
   **açıkça emekli** olur (iddianın yerine "bağlı satırda digest NULL" gelir). Reddedilmiş satır kolu
   aynen kalır.
1b. **Digest kopyalayan definer gövdeleri aynı imzalarla `CREATE OR REPLACE`** (S1, YÜKSEK):
   `identity_set_password`, `identity_consume_reset`, `identity_consume_row_reset`,
   `identity_link_legacy`, `identity_relink_refused`, `identity_accept_invite`,
   `identity_accept_invite_new`, `identity_claim_signup` artık satırlara digest kopyalamaz; yeni bağlı
   satırlar `password_hash NULL` doğar. **`identity_unlink_backfilled`** de (T3): contract sonrası çözülen
   satıra nöbetçi değil `NULL` yazar. Her yeniden tanım `SECURITY DEFINER`, `search_path`, sahip ve
   yetkileri açıkça yeniden yazar (T5). İmza katalog testi değişmez.
2. Bayat digest kopyaları `NULL` (yeni tetikleyiciyle uyumlu); `password_hash` NULL'lanabilir.
3. `DROP FUNCTION resolve_admin_by_email`, `identity_create_for_signup` (R8) ve `identity_reconcile`
   (T3).
4. `tappa_app`'tan `INSERT (email, password_hash)` ve `UPDATE (password_hash)` REVOKE; owner ve davetli
   satırlarını yalnız definer yazar (matris testi).
5. `CHECK (identity_id IS NOT NULL OR created_at < '<contract B anı>')` **VALID** (R10); test:
   contract'tan sonra bağlanmamış eski bir satırı `disabled` yapmak (M14-16) geçer, yeni bir bağsız satır
   reddedilir.
- **Contract A binary'si bu şemada** çalışır (rollout penceresi). **Kabul testi** (S1): contract B
  şemasında contract A binary'siyle parola değiştir, sıfırla (kimlik linki), davet kabul et (iki yol),
  kaydol (iki yol), hesap sayfasından bağla, pencere içinde çöz ve geri bağla → hepsi yeşil; bağlı
  satırlarda digest `NULL`; çözme yalnız `created_at < contract anı` satırlarda (CHECK ile tutarlı).
- Down kimlik digest'ini satırlara geri kopyalar, eski tetikleyiciyi geri koyar ve davetle doğmuş NULL
  satırları doldurmadan `NOT NULL`'u geri getirmez (ADR 0031 §8).

---

## P7 — Faz sonu

## M14-21 — Faz sonu doğrulaması

- **Bağımlılık:** hepsi
- **Kırmızı çizgi:** §4 tümü
- **Araç:** denetçi + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `docs(plan): close M14`

**Kabul kriterleri.** Tam `make check` (bir kez) · tam `-race` · M14 mutasyon seti · `make audit` (R5
WARN'ında yalnız beş kimlik tablosu + öncekiler) · `pg-restore-verify.sh` · üç genişlikte ekran turu ·
güvenlik merceği: tenant sınırını geçen her yol tek tabloda, her biri bir sırra anahtarlı (iki
adlandırılmış istisna hariç) ve testli · ADR 0031 "uygulandı" notu · state.md kapanışı.

---

## Riskler

| Risk | Etki | Azaltma |
|---|---|---|
| Çalınan panel çerezi kişinin **bütün** işletmelerine geçer | Etki alanı büyür | **Kabul edilen risk** (ADR 0031 §10); `authenticated_at` geçişte taşınır; oturumun mutlak ömrü + hassas işlemde yeniden doğrulama → backlog |
| Eski sekmeden yanlış işletmeye yazma | Veri yanlış tenant'a | M14-10a (geçişten **önce**) |
| Bayrak açık + M14-17 yok → ucuz numaralandırma kahini | Adres varlığı sızar | M14-17, M14-19'un bağımlılığı |
| Eski pod / geri alma satıra yazar, kimlik bayatlar | Eski (sızmış) parola geçerli olur | Bağlı satır tetikleyicisi (fail-closed, penceredeki 500 adlandırılmış bedel); uzlaştırma; M14-19 ön kontrolleri |
| Çift e-postalı canlı satırlar çok ve parolalar farklı | Destek yükü | M14-02; M14-18 bağlama; K-3 sayıyla yeniden |
| Daha önce kaydolmuş saldırgan (incumbent) | Kimlik saldırganın | Sıfırlama + onay penceresi (ilk doğrulamada ve her sıfırlamada); çözülen satır kalıcı ret + nöbetçi digest (ADR 0031 §5.8) |
| Onay penceresinde çalınmış kimlik çerezi kişiyi kendi işletmelerinden çözer | Kalıcı kopuş, sahipsiz tenant | Çözme taze oturum + parola ister, pencere içinde geri alınır, son owner uyarısı (S4) |
| Contract B'den sonra definer'lar satırlara digest kopyalamaya devam eder | 500 ya da değişmez kırılır | Contract B gövdeleri aynı imzayla yeniler (S1) |
| Tıklayan ≠ parolayı bilen (kurbanın tıklamasıyla saldırgan parolalı kimlik) | Doğrulanmış sahte kimlik | Kayıt onayı parolayı yeniden ister; doğrulama token'ı isteyen oturuma bağlı (ADR 0031 §5.10, §5.2) |
| Çalınmış eski çerezle bağlama | Kalıcı ele geçirme | `identity_link_legacy`'de iki oturumda 5 dakikalık tazelik |
| Bayrak kapalıyken bağlı/bağlanmamış farkı görünür | Ucuz numaralandırma kahini | Bayrak tablosu + bayt-aynı yanıt kriteri (M14-08) |
| "Çıkmadan geçiş" P5'i bekler | Özellik gecikir | Kullanıcıya özet notu (R23) |
| Audit satırı karşı tenant'ı / sayıyı sızdırır | §4.5 ruhu | Bayt-aynı satırlar + tarama testi |
| `tappa_app` kimlik bağını yazar hale gelir | Rızasız üyelik | Sütun GRANT testi + tetikleyici + mutasyon |
| Contract eski pod'u kırar | Kesinti | Contract A (kod) ve B (şema) ayrı deploy |
| Yeni definer rolü canlı kümede unutulur | Migration Job durur | Runbook adımı migration'dan önce |
| CI süresi | Kuyruk | Yeni DB testleri `internal/db` / `internal/adminauth`'ta; `cmd/tappa`'ya test eklenmez |
| E-posta doğrulaması kayıt terkini artırır | Onboarding | Tek link, açık kopya; ölçüm M14-17 |

## Canlıya alma planı

1. **Önce:** kullanıcının *"başla"*sı → M14-01; M14-02 sayıları; canlı kümeye `tappa_identitydefiner`
   (runbook, `tappa_owner`) — migration'dan önce.
2. **Deploy 1 — expand** (P1 + P2 + P3 + P4, bayrak `off`): migration Job rollout'tan önce koşar,
   eski pod birkaç dakika yeni şemada hizmet verir — expand eklemeli; eski pod'un bağlı satıra parola
   yazması tetikleyiciyle 500 (adlandırılmış bedel). Var olan panel oturumları etkilenmez.
3. **Deploy 2 — uzlaştırma** (M14-06b).
4. **P5 kartları** tek tek, bayrak `off` (M14-14, M14-17, M14-18, M14-15, M14-16): yüzeyleri bayrakla
   kapalı, yalnız M14-17'nin kayıt doğrulaması açık (iki adres için aynı ekran). M14-17'nin deploy'u
   `identity_create_for_signup`'ı çağırmayı bırakır ama onu **düşürmez**.
5. **Bayrak `on`** (M14-19): uzlaştırma yeniden + ön kontroller + `05-config.yaml` commit'i + deploy.
   Geri dönüş: `off` commit'i + deploy (dual-write sayesinde parolalar doğru).
6. **Soak:** K-3'ün 60 günü `max(bayrak, M14-14 ve M14-18 canlı)` = bu sırada **bayrağın açıldığı
   gün**; haftalık sayımlar ve SigNoz sayaçları.
7. **Contract A** (M14-20a, kod; şema aynı) → **Contract B** (M14-20b, bir sonraki deploy: tetikleyici
   değişimi, digest NULL, iki DROP, REVOKE, `created_at`'li VALID CHECK).

Her `main` birleştirmesi kullanıcı kararıdır. Hiçbir adım tap yoluna ya da çalışan oturumlarına
dokunmaz; kesinti yalnız bugünkü tek replikalı rollout'un kendi davranışıdır (`20-app.yaml:53-66`).
