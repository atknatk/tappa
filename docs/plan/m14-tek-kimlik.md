# M14 — Tek kimlik, çok işletme: panel yöneticisinin global kimliği ve işletme değiştirici

> **Durum:** plan (2026-10-21) — **başlanmadı, kullanıcı kararları bekliyor** (aşağıda *"Açık
> kararlar"*, K-1 … K-14). Canlı durum yalnız [state.md](state.md)'dedir; bu dosyaya "yapıldı"
> yazılmaz.
>
> **Mimari karar:** [ADR 0031](../adr/0031-tek-kimlik-cok-isletme.md) (durum: önerilen). Okumadan
> başlama: §2 (bugünkü model, kanıtlı), §5 (önerilen tasarım A′), §6 (§4.5 nasıl korunur), §8
> (expand → bayrak → contract).
>
> **Kullanıcı kararı (2026-10-21):** *"Hesapları birleştir (tek kimlik)"* — bir kişi = tek giriş
> kimliği, birden çok işletmeye üye; KF ↔ KM arasında çıkış yapmadan geçiş.

## Özet

- **Kişi** (`admin_identities`, tenant-kapsamsız): e-posta (global tekil), parola, e-posta
  doğrulaması, kilit. Uygulama tabloyu **göremez**; yalnız dar SECURITY DEFINER fonksiyonlar.
- **Üyelik** = bugünkü `admin_users` satırı + `identity_id`. `id` değişmez → bütün aktör
  referansları, FK'ler, RLS, rol kapıları aynen.
- **Oturum** = bugünkü `admin_sessions`, tek tenant'a damgalı, **değişmez**. Geçiş = yeni oturum
  (yeni token) hedef tenant'ta, eskisi iptal; tek definer çağrısı, iki tenant'ta birbirini anmayan
  iki audit satırı.
- **Birleştirme** yalnız kişi iki kimlik bilgisini de kanıtlayınca (girişte aynı parola otomatik;
  farklıysa hesap sayfasından eski parola). Operatör birleştiremez.
- **Geçiş:** expand migration + bayrak kapalı → bayrak açık (ConfigMap) → soak → contract (tek
  yönlü). Var olan oturumlar her adımda yaşar.

## Çalışma modu (CLAUDE.md §10) — her kart için

- **Yapıcı + ayrı üçüncü göz.** Her kartı bir yapıcı alt ajan (opus) uygular; işi yapmayan **ayrı**
  bir denetçi kabul kriterlerini tek tek doğrular. Bulgu varsa düzeltilir ve **yeni** bir denetçiyle
  yeniden denetlenir; onay gelmeden kart `done` olmaz. Bu milestone'un **neredeyse her kartı §4.5'e
  değer** → `tappa-security-auditor` varsayılan olarak koşar (istisnalar kartta yazılı).
- Her yeni tablo/sorgu/fonksiyon agent **`tappa-db-migrator`** ile (migration up+down, RLS,
  sqlc, izolasyon testi birlikte). Her UI işi skill **`tappa-brand`** okunarak.
- **Testler:** tur içinde yalnız **değişen** testler `-run` ile + saniyelik kapılar (`gofmt`,
  build, `bash scripts/redline-check.sh`). Tam `-race`, `go test ./...`, mutasyon seti ve
  `make check` bir kartta **bir kez**, commit'ten önceki son doğrulamada. Brief'lere yazılır.
- `make gen` her `.templ` / `db/queries/*.sql` değişikliğinden sonra; üretilen dosyalar commit edilir.
- **Uygulanmış migration değişmez** (§6); her değişiklik yeni dosya. Bugün son migration `00063`.
- Henüz var olmayan testin adı belgeye yazılmaz (`TestEveryNamedTestExists`); testi **tarif et**,
  adını yapıcı koyar.
- **Dal:** kullanıcının açacağı feature dalı (öneri: `m14-identity`). `main`'e birleştirme bir
  deploy kararıdır — kullanıcı istemedikçe yok. Expand ve contract **ayrı** birleştirmelerdir.
- **Kart bitince:** `state.md` ledger + "ŞU AN" + oturum notu.

## Görev listesi

Boyut: **S** ≈ yarım gün · **M** ≈ 1 gün · **L** ≈ 2–3 gün · **XL** ≈ 4+ gün (denetim turları dahil
kaba tahmin; bu repoda güvenlik kartları ortalama 3–6 tur sürdü).

| ID | Faz | Görev | Boyut | Araç / ajan | Bağımlılık |
|---|---|---|---|---|---|
| M14-01 | P0 | ADR 0031 kabulü + CLAUDE.md §4.5/§6 metni + ADR 0002/0013/0015/0025 notları + open-questions | S | orkestratör | kullanıcı kararları |
| M14-02 | P0 | Üretim ölçümü (salt sayı): e-posta grupları, çift satırlar, e-postasız satır, canlı oturum | S | yapıcı + kullanıcı (küme erişimi) | — |
| M14-03 | P1 | `tappa_identitydefiner` rolü + `admin_identities` + `admin_users.identity_id` (sütun GRANT kapalı) | M | `tappa-db-migrator` + güvenlik | M14-01 |
| M14-04 | P1 | Definer fonksiyonları I: çözümleyici, oturum açma, üyelik listesi, geçiş, toplu iptal | L | `tappa-db-migrator` + güvenlik | M14-03 |
| M14-05 | P1 | Definer fonksiyonları II: parola okuma/yazma (dual-write), sıfırlama tablosu + çözümleyici + tüketim, eski satırı bağlama | L | `tappa-db-migrator` + güvenlik | M14-03 |
| M14-06 | P1 | Backfill migration (idempotent, incumbent-first) + Down | M | `tappa-db-migrator` + güvenlik | M14-02, M14-03 |
| M14-07 | P2 | `adminauth`: kimlik doğrulaması + eski kol + girişte otomatik bağlama; doğrulanmış-kimlik tipi | L | yapıcı + güvenlik | M14-04, M14-05 |
| M14-08 | P2 | `adminauth`: Open / Switch / RevokeAll / ChangePassword kimlik üstünden + `TAPPA_ADMIN_IDENTITY` bayrağı + yazma yollarında dual-write | M | yapıcı + güvenlik | M14-07 |
| M14-09 | P3 | Giriş handler'ı: bayrak açıkken kimlik yolu, son kullanılan işletmeye iniş, seçici yalnız gerektiğinde | L | yapıcı + güvenlik | M14-08 |
| M14-10 | P3 | `POST /admin/switch` + `GET /admin/switch` + oran sınırı + CSRF + askı istisnası + iki audit | M | yapıcı + güvenlik | M14-08 |
| M14-11 | P3 | "Her yerden çık" + parola değişiminde bütün işletmelerde iptal | S–M | yapıcı + güvenlik | M14-08 |
| M14-12 | P4 | Kabukta işletme adı + değiştirici (S1/S7, üç genişlik) | M | yapıcı (`tappa-brand`) | M14-10 |
| M14-13 | P4 | Değiştirici sayfası + Choose ekranı metni | S–M | yapıcı (`tappa-brand`) | M14-10 |
| M14-14 | P5 | Parola sıfırlama kimlik başına | L | yapıcı + güvenlik | M14-05, M14-08 |
| M14-15 | P5 | Yönetici daveti (M7-07 / EM-12'yi devralır): var olan kimlik / yeni kimlik | XL | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-08, M14-14 |
| M14-16 | P5 | Üyelik yönetimi: yöneticiyi devre dışı bırak, sahiplik devri, son-owner değişmezi | M–L | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-15 |
| M14-17 | P5 | Kayıt: e-posta doğrulaması (EM-11) + var olan adresle kayıt | L | `tappa-db-migrator` + yapıcı + `tappa-brand` + güvenlik | M14-08, M14-14 |
| M14-18 | P5 | Hesap sayfası: "Your businesses" + eski işletmeyi bağla formu | M | yapıcı (`tappa-brand`) + güvenlik | M14-07, M14-12 |
| M14-19 | P6 | Bayrağı açma runbook'u + canlı backfill doğrulaması + izleme sayıları | S–M | yapıcı + kullanıcı | M14-09 … M14-13 |
| M14-20 | P6 | Contract migration: eski kol, `resolve_admin_by_email`, seçici blob'u, dolgu ve `MaxCandidates` emekli | L | `tappa-db-migrator` + yapıcı + güvenlik | M14-19 + K-3 son tarihi |
| M14-21 | P7 | Faz sonu doğrulaması | M | denetçi + güvenlik | hepsi |

**Toplam kaba tahmin:** ~25–35 iş günü (tek hat); paralel hatlarla ~15–20.

### Paralel hatlar

```
P0  M14-01 ─┬─────────────────────────────────────────────────────────────
    M14-02 ─┤ (kullanıcıyla, P1'e paralel)
P1          └─ M14-03 ─┬─ M14-04 ─┐
                       ├─ M14-05 ─┼─ M14-07 ─ M14-08 ─┬─ M14-09 ─┐
                       └─ M14-06 ─┘                   ├─ M14-10 ─┼─ M14-12 ─┐
                                                      ├─ M14-11  │  M14-13  │
                                                      ├─ M14-14 ─┼─ M14-15 ─ M14-16
                                                      │          └─ M14-17
                                                      └──────────── M14-18 ┘
P6  M14-19 (P3+P4 bitince) ─ soak ─ M14-20 (K-3 son tarihi)
P7  M14-21
```

- **Hat A (şema):** M14-04 ve M14-05 M14-03'ten sonra ayrı worktree'lerde paralel (ayrı migration
  numaraları önceden ayrılır: çakışmayı önlemek için orkestratör iki numarayı kart açılırken atar).
- **Hat B (oturum yüzeyi):** M14-09, M14-10, M14-11 M14-08'den sonra paralel; M14-12/13 M14-10'u bekler.
- **Hat C (e-posta yüzeyi):** M14-14 → M14-15 → M14-16; M14-17 M14-14'ten sonra Hat C'ye paralel.
  Hat C, **expand deploy'unu bloklamaz**: P5 kartları bayrağın arkasında ayrı ayrı sevk edilebilir.
- **Birleştirme noktaları:** expand deploy'u = P1 + P2 + P3 + P4 (bayrak kapalı sevk edilir, sonra
  açılır); contract deploy'u = M14-20, ayrı.

---

## Açık kararlar

> ✅ **Cevaplandı — 2026-10-21:** kullanıcı *"Önerilerin hepsini kabul"* dedi → K-1 … K-14'ün hepsinde
> **(a)**. Uygulama başlamadı; M14-01 kullanıcının *"başla"* demesini bekler.

Her biri kullanıcıya **AskUserQuestion** ile sorulur; cevap tarihiyle `state.md`'ye ve ADR 0031'e
yazılır. ADR 0031 §5 önerilerin seçildiğini varsayar.

1. **K-1 — E-posta tekilliği.** (a) Kimlik başına **global tekil** e-posta (citext, büyük/küçük harf
   farksız). (b) Tenant başına tekil kalsın, kimlik ayrı bir anahtarla tanınsın. — **Öneri: (a).**
   Tek kimliğin anlamı budur; giriş çözümleyicisi ≤ 1 satıra döner ve `MaxCandidates`/dolgu/seçici
   sayım kanalı emekli olur. (b) bugünkü bütün kanalları korur.
2. **K-2 — Var olan çift hesaplar nasıl birleşir.** (a) Yalnız kişi iki kimlik bilgisini de
   kanıtlayınca: girişte aynı parola ikisini de doğrularsa otomatik; farklıysa hesap sayfasından
   öbür işletmenin eski parolasıyla. (b) Aynı e-posta = otomatik birleştir. (c) Operatör birleştirir.
   — **Öneri: (a).** (b) `manager.go:285-292` / 00011 saldırısının kendisidir (kayıt herkese açık,
   e-posta doğrulaması yok); (c) operatörü kimlik otoritesi yapar (ADR 0020 ayrılığı, sosyal
   mühendislik).
3. **K-3 — Bağlanmamış eski satırlar için son tarih.** (a) Bayrak açıldıktan **60 gün** sonra
   contract; o gün hâlâ bağlanmamış satır varsa sayı kullanıcıya gösterilir ve karar yeniden sorulur
   (ör. o satırlar işletme sahibinin daveti ile yeniden kazanılır). (b) Contract süresiz ertelenir
   (iki giriş kolu kalıcı). (c) Son tarih yok, contract hemen (bağlanmamışlar kilitlenir). —
   **Öneri: (a).**
4. **K-4 — Girişte nereye inilir.** (a) Son kullanılan işletme; yoksa en eski aktif üyelik; seçici
   yok (değiştirici her zaman kabukta). (b) Birden çok üyelikte her girişte seçici (bugünkü gibi).
   (c) (a) + girişte kısa bir "şu işletmeye girdin — değiştir" notu. — **Öneri: (a).** Kullanıcının
   istediği "çıkmadan geçiş" seçiciyi gereksizleştirir; (c) tap ekranı değil, ama bir ekran daha.
5. **K-5 — Değiştiricinin yeri ve okuma bedeli** (D1: *"sayılar her sayfada bir kabuk okumasıdır"*,
   `account-auth.md:29`). (a) Kabukta mevcut işletmenin **adı** (TouchAdminSession'ın RETURNING'ine
   eklenir — 0 ek okuma) + "Switch business" bağlantısı → `/admin/switch` sayfası listeyi okur.
   (b) Kabukta açılır liste (her sayfa yüklemesinde 1 definer okuması). (c) S1 panosu gibi
   sayılarla (her sayfada N okuma). — **Öneri: (a).**
6. **K-6 — Devre dışı bırakma kimin ve neyin.** (a) Yalnız **üyelik başına**: işletme sahibi kendi
   işletmesindeki üyeliği kapatır, kişinin öbür işletmeleri etkilenmez; kimlik kilidi yalnız
   sistemin. (b) Ek olarak kimliği global kapatma (kimin yetkisiyle? → bir tenant'ın kararı öbür
   tenant'a ulaşır). — **Öneri: (a).**
7. **K-7 — Var olan bir kimliği başka işletmeye kim ekler.** (a) Yalnız o işletmenin owner'ı,
   **davetle**; davetli e-postasındaki linke tıklar ve **kendi parolasıyla** girerek kabul eder.
   (b) Owner doğrudan ekler (rıza yok). (c) Aynı holding'deki işletmelere otomatik. — **Öneri: (a).**
   (b) oltalama yüzeyi; (c) ADR 0027 `:881`, `:1146` ile çelişir.
8. **K-8 — Rol.** (a) İşletme başına (KF'de owner, KM'de manager olabilir). (b) Kimlik başına tek
   rol. — **Öneri: (a).** Bugünkü şema zaten böyle; policy motorunun `actor:role`'ü üyelikten okur.
9. **K-9 — Kayıt sihirbazı ve e-posta doğrulaması.** (a) Her yeni kayıt e-posta doğrulamasıyla
   biter (EM-11 M14'e katılır); var olan adresle kayıt aynı "gelen kutunu kontrol et" ekranını
   görür, link kişiyi kendi parolasıyla girip yeni işletmeyi sahiplenmeye götürür. (b) Doğrulama
   yok; var olan adresle kayıt "bu e-posta kayıtlı — giriş yap" der (sayılı numaralandırma kahini,
   ADR 0013'ün `signInBlocked`'ı gibi). (c) Var olan kimliğe sessizce bağla. — **Öneri: (a).**
   (c) saldırganın kurbanın değiştiricisine işletme eklemesine izin verir; (b) kahini kalıcılaştırır.
   Bedel: onboarding'e bir e-posta adımı.
10. **K-10 — Değiştiricide askıdaki işletme.** (a) Gösterilir, nötr bir "read-only" işaretiyle;
    geçiş serbest (ADR 0025: askıda giriş ve okuma açık). (b) Gizlenir. — **Öneri: (a).** Askıdaki
    işletmenin yöneticisi dışa aktarmaya erişebilmeli (ADR 0025 `:509-510`).
11. **K-11 — Sahiplik devri ve üyelik kaldırma M14'te mi.** (a) Evet (M14-16): davet açılınca
    "yöneticiyi çıkar" ve "son owner" kuralı zorunlu olur. (b) Ayrı milestone; M14 yalnız davet.
    — **Öneri: (a).**
12. **K-12 — Operatörün rolü.** (a) Değişmez: kimlik tablolarına hiçbir `op_*` yetkisi yok;
    birleştiremez, bağlayamaz; geçiş izlemesi runbook ile yalnız sayı. (b) Operatöre tenant
    detayında salt okunur "bağlanmamış eski satır sayısı" (yeni `op_*`, e-posta yok). — **Öneri:
    (a).** (b) istenirse ADR 0021 kapsamında ayrı küçük kart.
13. **K-13 — CLAUDE.md §4.5 sınıflandırması.** (a) Kimlik fonksiyonları ADR 0002 md.7 sınıfıdır
    (sırra anahtarlı, bağlam **üreten** çözümleyiciler; geçiş bağlamı aşmaz, yeni tek-tenant oturum
    üretir); §6'nın tenant-kapsamsız tablo listesine `admin_identities` ve iki küçük tablo eklenir.
    (b) Açıkça **üçüncü istisna** olarak yazılır ("istisnalar üç tanedir"). — **Öneri: (a)**, ama
    bu bir yorumdur ve §4.5'e değer; kullanıcı açıkça onaylamalı.
14. **K-14 — E-posta ve görünen ad nerede okunur.** (a) `admin_users.email` kimliğin **aynası**
    olarak kalır (yalnız kimlik fonksiyonu yazar), `full_name` üyelikte (işletme başına) kalır →
    işletme kendi ekibini RLS içinde okur. (b) İkisi de yalnız kimlikte; işletme ekibini bir definer
    okumasıyla görür. — **Öneri: (a).** Ekip listesi ve davet ekranı yeni bir tenant-ötesi okuma
    istemez.

---

## P0 — Kararlar ve ölçüm

## M14-01 — ADR 0031 kabulü ve kural metinleri

- **Bağımlılık:** K-1 … K-14 cevapları
- **Kırmızı çizgi:** §4.5 (metin değişikliği), §6
- **Araç:** orkestratör
- **Boyut:** S
- **Commit:** `docs(adr): accept ADR 0031 and amend the tenant-isolation rules`

**Amaç.** Kullanıcının cevaplarını ADR 0031'e işlemek, durumu "kabul edildi (tarih)" yapmak ve kural
metinlerini tek commit'te hizalamak.

**Dokunulacak dosyalar.** `docs/adr/0031-*.md` · `CLAUDE.md` §4.5 (K-13) ve §6 (tenant-kapsamsız
tablo listesi; tablo adları) · §3 dizin haritası (gerekirse `internal/adminauth` satırı) · ADR 0002
(md.7'ye tarihli not: giriş çözümleyicisi ≤ 1'e döner) · ADR 0013 (contract'ta emekli olacak
mekanizmalar) · ADR 0015 (yetkinin nesnesi kimlik) · ADR 0025 (istisna listesine `/admin/switch`) ·
`docs/plan/open-questions.md` (açık kalan kararlar) · `docs/backlog.md` (*"Tenant değiştirici"*
satırı → M14'e taşındı notu; API anahtarı yaratıcısı devre dışıyken davranış notu).

**Kabul kriterleri.**
- ADR'nin her `K-n` atfı cevaplanmış; reddedilen seçenekler gerekçesiyle ADR'de.
- CLAUDE.md değişikliği yalnız K-13'ün seçtiği metin; başka kural değişmez (diff ile gösterilir).
- `go test ./cmd/tappa/ -run 'EveryNamedTest' -count=1` yeşil.

## M14-02 — Üretim ölçümü (salt sayı)

- **Bağımlılık:** —
- **Kırmızı çizgi:** §7 (e-posta log'a/belgeye yazılmaz)
- **Araç:** yapıcı + kullanıcı (küme erişimi Tailscale üzerinden)
- **Boyut:** S
- **Commit:** `docs(plan): record the admin identity counts M14 starts from`

**Amaç.** Backfill'in ve K-3'ün dayanağı olan sayıları ölçmek: dolu e-postalı `admin_users` satırı;
`lower(email)` gruplarının boyut dağılımı (1, 2, 3+); e-postasız satır; `disabled` satır; canlı
(`revoked_at IS NULL`) panel oturumu; grup başına tenant'ların askı durumu.

**Kabul kriterleri.**
- Sorgu `tappa_owner` ile **salt okunur** bir transaction'da koşar; çıktı yalnız sayıdır (e-posta,
  ad, id yok); sorgu metni runbook'a, sayılar `state.md`'ye tarihle yazılır.
- Çift satırlı her grup için "parolalar aynı mı" **ölçülmez** (digest tuzlu; ölçülemez) — bu,
  girişte otomatik bağlamanın kapsamını kullanıcıya tahmin olarak değil bilinmeyen olarak bildirir.

**Tuzaklar.** Sayıları canlı iddia olarak yazma; tarih ve kaynak ile yaz.

---

## P1 — Şema ve RLS

## M14-03 — Kimlik tablosu, definer rolü, üyelik sütunu

- **Bağımlılık:** M14-01
- **Kırmızı çizgi:** §4.5 · §6 (tenant-kapsamsız tablo kuralları) · §4.6 (silme yok)
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(db): add admin identities and link memberships to them`

**Amaç.** ADR 0031 §5.1–5.2'nin tablo ve rol yarısı: `admin_identities`, `tappa_identitydefiner`,
`admin_users.identity_id` + `UNIQUE (tenant_id, identity_id)`.

**Dokunulacak dosyalar.** `scripts/db-init/01-roles.sql` (rol; migration'da `CREATE ROLE` yok) ·
`deploy/README.md` (canlı küme runbook adımı: rol + `GRANT` — `tappa_holdingdefiner` emsali) ·
yeni `db/migrations/000NN_create_admin_identities.sql` · `internal/db` katalog/RLS testleri.

**Kabul kriterleri.**
- Up bölümünde `-- redline: no-tenant-scope(admin_identities) — <gerekçe>`; `bash
  scripts/redline-check.sh` R5 WARN'ında tablo adı görünür, FAIL yok.
- `ENABLE` + `FORCE ROW LEVEL SECURITY` birlikte; `pg-restore-verify.sh`'nin ENABLE/FORCE sayı
  eşitliği korunur (manifest güncellenir).
- `tappa_app`: tablo ve sırada açık `REVOKE ALL`; `has_any_column_privilege('tappa_app',
  'admin_identities', …) = false` testle.
- `tappa_app` `admin_users.identity_id`'ye **INSERT ve UPDATE edemez** — sütun düzeyi GRANT
  (00017 kalıbı); test iki yazmanın da `permission denied` verdiğini gösterir; GRANT açılınca test
  kırmızı (mutasyon kanıtı).
- `email` citext **UNIQUE**; büyük/küçük harf farkıyla ikinci satır reddedilir (test).
- `password_hash` 00018'in bcrypt CHECK'iyle aynı biçim kuralı; `created_at` uygulamaya yazılamaz.
- Sütun adı `tenant_id` **değil** (`last_used_tenant`); R5/RLS testleri tabloyu tenant tablosu saymaz.
- **RLS izolasyon testi (filtresiz):** A tenant'ının bağlantısı aynı kimliğin B'deki `admin_users`
  satırını görmez; pozitif kontrol (B bağlamında görünür).
- Down: sütun, indeks, tablo düşer; Up → Down → Up temiz.
- Eski binary'nin `CreateAdminUser` INSERT'i (identity_id'siz) yeni şemada çalışır (expand uyumu).

**Tuzaklar.** `ALTER DEFAULT PRIVILEGES` yeni tabloya `tappa_app` için yetki verir (CLAUDE.md §6,
ölçüldü) — açık REVOKE şart. Rol migration'da yaratılırsa R5b kırılır (ADR 0021 `:759-763`).

## M14-04 — Definer fonksiyonları I: çözümleme, oturum, geçiş

- **Bağımlılık:** M14-03
- **Kırmızı çizgi:** §4.5 (tenant sınırını geçen tek hareket burada) · §4.6 (audit)
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(db): resolve identities and switch a panel session between memberships`

**Amaç.** ADR 0031 §5.2 tablosunun ilk beş fonksiyonu: `resolve_identity_by_email`,
`identity_open_session`, `identity_memberships`, `identity_switch_session`,
`identity_revoke_sessions`; elle yazılmış Go çağıranları (`internal/db/resolve.go` emsali —
sqlc `RETURNS TABLE` tipleyemez, ADR 0002 `:300-315`).

**Kabul kriterleri.**
- Her fonksiyon: SECURITY DEFINER, `SET search_path = pg_catalog, pg_temp`, şema-nitelikli gövde,
  dinamik SQL yok, sabit sütun listesi, `REVOKE ALL FROM PUBLIC`, `EXECUTE` yalnız `tappa_app`.
  Çözümleyicinin sahibi `tappa_resolver`, öbürlerinin `tappa_identitydefiner`; katalog testi
  `prosecdef` sahiplerinin izinli kümesini genişletir ve `rolsuper=f`, rol üyesi 0.
- `resolve_identity_by_email` ≤ 1 satır (UNIQUE'ten), citext eşitliği `OPERATOR(public.=)`.
- `identity_switch_session`:
  - hedefte aynı `identity_id`'li `active` üyelik yoksa 0 satır; `disabled` üyelik → 0; iptal
    edilmiş mevcut oturum → 0; başka kimliğin oturumu → 0 (her biri ayrı test);
  - başarıda: hedefte yeni oturum (verilen hash), mevcut oturum `revoked_at`, `last_used_tenant`
    güncel, **iki** `audit_log` satırı (A ve B) — ve hiçbir satırın `detail`'i karşı tenant'ın id'sini
    ya da adını taşımaz (metin taraması testi);
  - **eşzamanlılık:** aynı oturumla N paralel geçiş → tam olarak bir yeni canlı oturum, `-race`.
- `identity_memberships` yalnız canlı oturumun kimliğinin aktif üyeliklerini döner, LIMIT 50,
  askı bilgisiyle; başka kimliğin üyeliği hiçbir girdiyle dönmez.
- **Mutasyon kanıtları:** kimlik eşitliği yüklemi silinince, `status='active'` silinince, eski
  oturum iptali silinince, audit'e hedef tenant eklenince ilgili test kırmızı.
- Hiçbir fonksiyon token, digest (çözümleyici hariç) ya da e-posta (çözümleyici girdisi hariç)
  döndürmez.

**Tuzaklar.** citext eşitliğinin `public` şemasında olması (ADR 0002 `:125-140`). Audit satırı
yazan definer'ın `audit_log`'a yazma yetkisi sütun düzeyinde ve yalnız INSERT. `identity_open_session`
anahtarı bir sır değildir — çağrının tek yolu M14-07'nin doğrulanmış-kimlik tipidir (ADR 0031 §5.2);
bu kart bunu SQL'de iddia etmez.

## M14-05 — Definer fonksiyonları II: parola, sıfırlama, eski satırı bağlama

- **Bağımlılık:** M14-03
- **Kırmızı çizgi:** §4.5 · §4.7 (token yalnız hash olarak) · §4.6
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(db): set identity passwords, reset them by token and link legacy rows`

**Amaç.** `identity_password_for_session`, `identity_set_password` (dual-write), tenant-kapsamsız
`admin_identity_resets` + `resolve_identity_reset_by_token_hash` + `identity_consume_reset`,
`identity_link_legacy`.

**Kabul kriterleri.**
- `admin_identity_resets`: M14-03'ün tenant-kapsamsız kuralları; 00019'un TTL tavanı ve token-hash
  biçim CHECK'i; `used_at` / `cancelled_at`; DELETE yok.
- `identity_set_password` digest'i kimliğe **ve** bağlı her `admin_users.password_hash`'e yazar
  (dual-write, contract'a kadar), öbür oturumları **bütün** işletmelerde iptal eder, mevcut oturumu
  korur; her etkilenen tenant'a kendi audit satırı (karşı tenant'ı anmadan).
- `identity_consume_reset` tek kullanımlık (ikinci tüketim 0 satır), oturum vermez, bütün oturumları
  iptal eder, `email_verified_at`'i doldurur, eski (bağlanmamış) satırları **bağlamaz** (test).
- `identity_link_legacy`: yalnız (canlı oturumun kimliği) + (aynı e-postalı, `identity_id IS NULL`
  bir satır) için; satır zaten bağlıysa ya da başka e-postalıysa 0; bağlanınca o tenant'a audit
  "linked". Go tarafının parola kanıtı M14-07'dedir.
- Mutasyon kanıtları: dual-write kolu silinince, tüm-işletme iptali tek tenant'a daraltılınca,
  tek kullanımlık yüklem silinince test kırmızı.

## M14-06 — Backfill

- **Bağımlılık:** M14-02, M14-03
- **Kırmızı çizgi:** §4.5 · §4.6 (satır silinmez)
- **Araç:** agent `tappa-db-migrator` + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(db): backfill admin identities, oldest account first`

**Amaç.** ADR 0031 §8 adım 2: her dolu e-posta grubu için kimlik; tek satırlı grup bağlanır, çok
satırlı grupta yalnız **en eski** (`created_at, tenant_id`) satır bağlanır; e-postasız satır
dokunulmaz.

**Kabul kriterleri.**
- İdempotent: iki kez koşmak ikinci kimlik yaratmaz (test).
- Seed'de KF/KM owner'ları ve elle eklenen çift-e-posta fikstürüyle: beklenen kimlik sayısı, bağlı
  satır sayısı, bağlanmamış satır sayısı birebir.
- Saldırı fikstürü: kurbanın e-postasını daha **sonra** taşıyan satır bağlanmaz.
- Down: bağları çözer ve kimlikleri siler — yalnız bu migration'ın yarattıklarını (dual-write
  sayesinde `admin_users.password_hash` güncel kalır, veri kaybı yok) — Up → Down → Up temiz.
- Büyük tabloda `lock_timeout` (CLAUDE.md §4.6 notu); M14-02'nin sayısıyla süre tahmini yazılı.

---

## P2 — Domain / auth

## M14-07 — Kimlik doğrulaması ve otomatik bağlama

- **Bağımlılık:** M14-04, M14-05
- **Kırmızı çizgi:** §4.5 (00011 YÜKÜMLÜLÜK 1, 2, 5) · §7
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): authenticate a person by identity and link proven legacy rows`

**Amaç.** `internal/adminauth`'ta: e-posta → kimlik → tek bcrypt; eski kol (bağlanmamış satırlar,
bugünkü 8'li dolgulu karşılaştırma); aynı parola hem kimliği hem eski satırı doğrularsa bağlama;
doğrulanmış kimliği taşıyan ve `identity_open_session`'ın **tek** çağrı yolu olan tip.

**Kabul kriterleri.**
- Bilinmeyen e-posta, yanlış parola, devre dışı üyelik, kilitli kimlik **tek** hata değerine çöker;
  zamanlama testi (bugünkü `TestPanelE2E_TimingIsFlatOverHTTP` deseni) kimlik yolunda tek tip.
- `TestAuthenticate_RefusesTheCrossTenantBypass`'ın senaryosu kimlik yolunda yeniden koşar ve
  saldırganın ektiği satırın kurbanın kimliğine **bağlanmadığını** ve oturum açmadığını gösterir.
- Yalnız eski satır doğrularsa kişi o satıra bugünkü gibi girer (kimliksiz); kimlik açılmaz.
- Doğrulanmış-kimlik tipinin dışarıda kurulamadığı (ya da kurulabildiği her yolun adlandırıldığı)
  bugünkü `Verified` iki-bacak dürüstlüğüyle yazılı (`manager.go:251-292` deseni).
- Log'da e-posta yok; `identity_id` ADR 0022 §10 kümesine eklenir.

## M14-08 — Oturum işlemleri, bayrak ve dual-write

- **Bağımlılık:** M14-07
- **Kırmızı çizgi:** §4.5 · §4.7
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(auth): open, switch and revoke panel sessions through the identity`

**Amaç.** `Open`, `Switch`, `RevokeAll`, kimlik üstünden `ChangeOwnPassword`; config
`TAPPA_ADMIN_IDENTITY` (`off` | `on`, varsayılan `off`, `internal/config`'te doğrulanır); yazma
yolları (kayıt, parola değişimi, sıfırlama) bayraktan **bağımsız** olarak kimliği de günceller.

**Kabul kriterleri.**
- Bayrak `off`: davranış bugünküyle bayt-aynı (giriş/seçici testleri değişmeden yeşil);
  kayıt yeni kimlik + bağlı satır yaratır; parola değişimi kimliği de yazar.
- Bayrak `on` ↔ `off` arasında gidip gelmek hiçbir kullanıcının parolasını geçersiz kılmaz (test:
  `on`'da değiştir → `off`'ta yeni parolayla gir).
- `Switch` yeni token üretir; eski token artık çözülmez; `Verify` yolu değişmemiştir (diff).
- `TouchAdminSession` RETURNING'ine `tenants.name` eklenir; per-request okuma sayısı değişmez.

---

## P3 — Giriş, geçiş, oturum

## M14-09 — Giriş handler'ı (kimlik yolu)

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5 · §7
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): sign a person in to their last business`

**Amaç.** `POST /admin/login` bayrak açıkken kimlik yolunu kullanır; K-4'e göre iniş; seçici yalnız
K-4 (b/c) seçilirse; oran sınırları (`adminAttemptLimit`, `adminLoginWorkLimit`, `adminFloodLimit`)
aynen.

**Kabul kriterleri.**
- Tek üyelikli kişi doğrudan `/admin`; çok üyelikli kişi `last_used_tenant`'a; o üyelik devre
  dışıysa en eski aktif üyeliğe.
- Başarısızlık gövdesi senkronizör token dışında bayt-aynı (`failLogin` disiplini).
- Bayrak `off` yolunda mevcut testler (`TestAdminLogin_OneVerifiedBusinessSkipsThePicker`,
  `TestAdminChoose_RefusesATenantOutsideTheSignedSet` …) değişmeden yeşil.
- Başarılı girişin audit satırı yalnız inilen tenant'ta.

## M14-10 — Geçiş rotaları

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5 · ADR 0025 (istisna listesi)
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(panel): switch businesses without signing out`

**Amaç.** `GET /admin/switch` (üyelik listesi) ve `POST /admin/switch` (geçiş); ADR 0031 §5.4.

**Kabul kriterleri.**
- POST zinciri `floodGate → sameOriginGate → requireAdmin → sessionGate` + senkronizör token;
  çapraz kökenli ve token'sız istek reddedilir (bugünkü `TestAdminAuth_RefusesForgedAndCrossSiteRequests`
  deseniyle).
- `suspensionGate` bu rotada **yok**; ADR 0025'in isimli istisna sayısı bir artar ve onu sayan test
  aynı commit'te güncellenir (iddia gevşetilmez).
- Hedef reddi tek tip sayfa (var olmayan / üye olunmayan / devre dışı ayırt edilmez).
- Oturum başına ayrı oran kovası; aşılınca 429 ve audit yok (audit şişirme kapalı).
- Rota `/admin` altında (çerez yolu — `TestAdminRoutes_AreAllUnderTheCookiePath`).
- Askıdaki hedefe geçiş başarılı; sonraki POST askı kapısına takılır.

## M14-11 — Her yerden çık

- **Bağımlılık:** M14-08
- **Kırmızı çizgi:** §4.5
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** S–M
- **Commit:** `feat(account): sign out of every business at once`

**Kabul kriterleri.** Hesap sayfasında "Sign out everywhere" bütün işletmelerdeki oturumları iptal
eder; parola değişimi öbür oturumları bütün işletmelerde iptal eder (bugün yalnız tek tenant'ta —
`changepassword.go:110`); her tenant'a kendi audit satırı.

---

## P4 — Panel arayüzü

## M14-12 — Kabukta işletme adı ve değiştirici

- **Bağımlılık:** M14-10
- **Kırmızı çizgi:** §9 (tenant markası slotları değişmez)
- **Araç:** yapıcı, skill `tappa-brand`
- **Boyut:** M
- **Commit:** `feat(panel): show the current business and a switch link in the shell`

**Amaç.** M12 panoları S1 / S7'deki değiştiriciyi K-5'in seçtiği biçimde geri getirmek: masaüstü yan
menü başı, tablet ikon şeridi, telefon "More" sayfası; tek DOM.

**Kabul kriterleri.**
- İşletme adı her tenant'ta görünür (bugün yalnız markalıda — `admin.templ:456-460`,
  `panelbrand.go:108-116`); kaynak `TouchAdminSession`'ın RETURNING'i, ek okuma yok.
- K-5 (a): "Switch business" bağlantısı. Yalnız birden çok üyelikte göstermek üyelik sayısını
  bilmeyi, yani bir okumayı ister; iki yol var: bağlantı her zaman görünür (tek üyelikte sayfa "tek
  işletmen var" der) ya da sayı oturum açılırken/geçişte damgalanır. Yapıcı ikisini ölçer, seçimi
  ve gerekçesini rapora yazar; sayfa başına ek okuma 0 kalır.
- Dokunma hedefi ≥ 44 px, durum yalnız renkle değil, tek `<h1>`; `TestUnbrandedScreens_AreByteIdenticalToTheGolden`
  ve marka CSS golden'ı bayt-aynı.
- Tenant accent slotları büyümez (CLAUDE.md §9).

## M14-13 — Değiştirici sayfası ve Choose metni

- **Bağımlılık:** M14-10
- **Kırmızı çizgi:** §9
- **Araç:** yapıcı, skill `tappa-brand`
- **Boyut:** S–M
- **Commit:** `feat(panel): list a person's businesses on the switch page`

**Kabul kriterleri.** `/admin/switch` Choose panolarının adisyon düzeniyle (işletme adı, rol,
"Continue to {name}"; askıdaysa K-10 işareti); Choose ekranı (eski kol ve K-4 b/c için) *"you can
sign out and switch at any time"* yerine çıkmadan geçişi anlatır; üç genişlik.

---

## P5 — Sıfırlama, davet, hesap, kayıt

## M14-14 — Parola sıfırlama kimlik başına

- **Bağımlılık:** M14-05, M14-08
- **Kırmızı çizgi:** §4.7 · §7 · ADR 0015 · ADR 0022
- **Araç:** yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(auth): reset a person's password once for every business`

**Kabul kriterleri.**
- Bir adres → en çok **bir** canlı link (bugün en çok 8 — `resetrequest.go:157-224`); kayıtlı ve
  kayıtsız adres yanıt ve zamanlamada ayırt edilemez (EM-5'in asenkron teslim deseni aynen).
- Tüketim kimliği yazar, bütün işletmelerdeki oturumları iptal eder, oturum vermez; eski satırları
  bağlamaz.
- Bağlanmamış eski satırlar geçiş döneminde bugünkü satır-başı sıfırlamayı korur.
- "Parolanız değişti" bildirimi (EM-9) kişi başına bir kez.

## M14-15 — Yönetici daveti (M7-07 / EM-12'yi devralır)

- **Bağımlılık:** M14-08, M14-14 (e-posta kanalı)
- **Kırmızı çizgi:** §4.5 (davet bir tenant'a kimlik ekler) · §4.6 · §4.7 · ADR 0005 Y-D'nin
  yönetici ikizi
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** XL
- **Commit:** `feat(auth): invite a manager by e-mail`

**Amaç.** M7-07 kartının (`m7-portal.md:1509-1591`) bütün kabul kriterleri, kimlik modelinde:
owner-only davet (policy motoru), tek kullanımlık + süreli + iptal edilebilir + hash'li token
(`admin_identity_tokens`), davetlinin adresine gider, panelde gösterilmez; kabulde var olan kimlik
**kendi parolasıyla** girer, yoksa parola belirler (yeni kimlik, `email_verified_at` dolu).

**Kabul kriterleri.** M7-07'nin altı maddesi + rızasız üyelik yaratan yol yok (test: davet kabul
edilmeden `admin_users` satırı 0) + davet, holding üyeliğinden türemez + baseline materyalize
edilmiş tenant'ta owner kilitlenmez (M6-12 B'nin ölçtüğü `deny sid=default` tuzağı — önce ölç).

**Tuzaklar.** M7-07'nin *"`resolve_admin_by_email` penceresi"* tuzağı kimlik yolunda ortadan
kalkar, eski kolda (bayrak `off`) hâlâ geçerlidir → davet yalnız bayrak `on` iken açılır.

## M14-16 — Üyelik yönetimi

- **Bağımlılık:** M14-15
- **Kırmızı çizgi:** §4.5 · §4.6 (silme yok)
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** M–L
- **Commit:** `feat(panel): disable a manager and transfer ownership`

**Kabul kriterleri.** Owner o işletmedeki üyeliği `disabled` yapar → yalnız o tenant'taki oturumlar
düşer, kişinin öbür işletmeleri etkilenmez (iki tenant'lı test); son aktif owner düşürülemez ve
indirilemez (DB düzeyinde ya da tek ifadeli yüklemle, eşzamanlı iki indirme testiyle); sahiplik
devri iki adım (yeni owner → eskiyi indir), her adım audit'te.

## M14-17 — Kayıt: e-posta doğrulaması ve var olan adres

- **Bağımlılık:** M14-08, M14-14 · **K-9**
- **Kırmızı çizgi:** §4.5 · §7 · ADR 0013
- **Araç:** agent `tappa-db-migrator` + yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `feat(signup): verify the owner's e-mail before the business is theirs`

**Kabul kriterleri (K-9 a).** Yeni ve var olan adres için yanıt ve zamanlama ayırt edilemez; var
olan adresle başlatılan kayıtta owner üyeliği yalnız adres sahibi **kendi parolasıyla** onaylayınca
doğar; onaylanmayan tenant'ın sahipsiz kalması ve süresi (operatör görünümü dahil) yazılı;
`signInBlocked` ve `signup.sign_in_unreachable` kimlik yolunda emekli (eski kolda contract'a dek
kalır).

## M14-18 — Hesap sayfası: işletmelerim ve bağlama

- **Bağımlılık:** M14-07, M14-12
- **Kırmızı çizgi:** §4.5 · §7
- **Araç:** yapıcı + skill `tappa-brand` + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `feat(account): list my businesses and link one by its old password`

**Kabul kriterleri.** "Your businesses" listesi `identity_memberships`'ten; "Link a business" formu
eski parolayı ister, aynı e-postalı bağlanmamış satırları (en çok 8, dolgulu) karşılaştırır,
doğrulananları bağlar; sonuç tek tip ("bağlanacak işletme bulunamadı" ile "parola yanlış" ayırt
edilmez); hesap başına oran sınırı (`adminAccountLimit` sınıfı).

---

## P6 — Canlıya geçiş ve contract

## M14-19 — Bayrağı açma

- **Bağımlılık:** M14-09 … M14-13 canlıda (bayrak `off`)
- **Kırmızı çizgi:** §7
- **Araç:** yapıcı + kullanıcı
- **Boyut:** S–M
- **Commit:** `docs(deploy): turn the admin identity on`

**Kabul kriterleri.** Runbook: ön kontrol (backfill sayıları M14-02 ile tutarlı) → ConfigMap
`TAPPA_ADMIN_IDENTITY=on` → rollout → duman testi (KF ve KM arasında geçiş, sign-out everywhere,
yanlış parola) → geri dönüş adımı (`off`) yazılı ve bir kez **denenmiş**; haftalık "bağlanmamış eski
satır" sayımı (yalnız sayı) state.md'ye.

## M14-20 — Contract

- **Bağımlılık:** M14-19 + K-3'ün son tarihi + kullanıcı onayı
- **Kırmızı çizgi:** §4.5 · §6 (tek yönlü migration'ın dürüstlüğü)
- **Araç:** agent `tappa-db-migrator` + yapıcı + agent `tappa-security-auditor`
- **Boyut:** L
- **Commit:** `refactor(auth): retire the per-business sign-in`

**Amaç.** Eski kolu kaldırmak: `resolve_admin_by_email` DROP, `MaxCandidates` / dolgu / `PickerCap`
/ imzalı seçici seti / `signInBlocked` / satır-başı sıfırlama emekli; `admin_users.password_hash`
NULL'lanabilir ve okunmaz; `tappa_app`'ın `UPDATE (email)`'i kapanır; bayrak kaldırılır.

**Kabul kriterleri.**
- Emekli olan her mekanizmanın testleri **silinmez, dönüştürülür**: korudukları iddianın kimlik
  yolundaki karşılığı yeşil (ör. sayım kanalı artık yapısal olarak yok → "e-posta ≤ 1 kimlik" testi).
- `manager.go:285-292` yorumu tarihsel olarak işaretlenir, ADR 0031'e bağlanır; `TestMaxCandidates_IsTheMeasuredCPUBound`
  ve seçici testleri ya kaldırılır ya yeni iddialarına yeniden adlandırılır — aynı commit'te,
  `TestEveryNamedTestExists` yeşil.
- Down yazılı ve **tek yönlülüğü** başlıkta: şemayı geri getirir, contract sonrası değişmiş
  parolaları geri getiremez (ADR 0010 dürüstlüğü).
- `adminLoginWorkLimit` yeniden ölçülür; değişiklik önerisi ayrı karar olarak rapora.

---

## P7 — Faz sonu

## M14-21 — Faz sonu doğrulaması

- **Bağımlılık:** hepsi
- **Kırmızı çizgi:** §4 tümü
- **Araç:** denetçi + agent `tappa-security-auditor`
- **Boyut:** M
- **Commit:** `docs(plan): close M14`

**Kabul kriterleri.** Tam `make check` (bir kez) · tam `-race` · M14 mutasyon setinin tamamı · `make
audit` (R5 WARN'ında yalnız beklenen tablolar) · `pg-restore-verify.sh` yeşil · üç genişlikte ekran
turu (giriş, geçiş, değiştirici, hesap, davet, sıfırlama) · güvenlik merceği: tenant sınırını geçen
her yol (giriş, geçiş, davet kabulü, bağlama, sıfırlama) tek tabloda, her biri bir sırra anahtarlı
ve testli · ADR 0031 durum satırı "uygulandı" notu · state.md kapanışı.

---

## Riskler

| Risk | Etki | Azaltma |
|---|---|---|
| Çift e-postalı canlı satırlar beklenenden çok ve parolaları farklı | Kullanıcılar ikinci işletmeyi kendi bağlamak zorunda; destek yükü | M14-02 önce ölçer; M14-18 formu; K-3 son tarihi sayıyla yeniden sorulur |
| Saldırganın daha **önce** kaydolduğu e-posta (incumbent saldırganın) | Kimlik saldırganın; gerçek kişi giremez | E-postaya sahiplik (M14-14 sıfırlama) kimliği geri alır; gerçek kişinin satırı kendi parolasıyla bağlanır — ADR 0031 §5.7 |
| Geçişin audit satırı karşı tenant'ı sızdırır | §4.5 ruhu; A'nın sahibi kişinin B'ye üyeliğini öğrenir | Metin taraması testi (M14-04) |
| `tappa_app` `identity_id` yazabilir hale gelir (yanlış GRANT / default ACL) | Rızasız üyelik | Sütun GRANT testi + mutasyon kanıtı (M14-03) |
| İki giriş kolu uzun yaşar | Karmaşıklık, iki kat test | K-3 son tarihi; contract ayrı ve planlı |
| Contract tek yönlü | Geri dönüş yok | Bayrak dönemi ≥ 60 gün; dual-write; contract kullanıcı onayıyla |
| Yeni definer rolü canlı kümede unutulur | Migration Job hata verir, deploy durur | Runbook adımı + migration'ın başında rol var mı kontrolü (`tappa_holdingdefiner` dersi) |
| CI süresi (cmd/tappa zaten ~20 dk -race) | Kuyruk | Yeni DB testleri `internal/db` ve `internal/adminauth`'ta; `cmd/tappa`'ya test eklenmez |
| E-posta doğrulaması onboarding'i yavaşlatır (K-9 a) | Kayıt terk oranı | Onay ekranı kopyası + tek link; ölçüm M14-17'de |

## Canlıya alma planı

1. **Önce:** M14-02 sayıları; kullanıcı kararları (M14-01); canlı kümeye `tappa_identitydefiner`
   rolü (runbook, `tappa_owner` ile) — migration'dan **önce**.
2. **Expand deploy** (`main`'e birleştirme = kullanıcı kararı): P1 migration'ları (tablolar →
   fonksiyonlar → backfill, hepsi eklemeli) + P2–P4 binary'si, bayrak `off`. Migration Job
   rollout'tan önce koşar; eski pod yeni şemaya karşı birkaç dakika hizmet verir — expand eklemeli
   olduğu için eski binary etkilenmez; o pencerede doğan kayıtlar tembel yolla bağlanır. Var olan
   panel oturumları etkilenmez (`admin_sessions` değişmedi).
3. **Bayrak `on`** (M14-19): yalnız ConfigMap; geri dönüş = `off`.
4. **P5 kartları** bayrak açıkken birer birer sevk edilir (davet, sıfırlama, kayıt doğrulaması).
5. **Soak** (K-3, öneri 60 gün) + haftalık sayım.
6. **Contract deploy** (M14-20): ayrı birleştirme, ayrı kullanıcı onayı.

Sıfır kesinti: hiçbir adım `admin_sessions`'a, per-request doğrulama yoluna ya da tap yoluna
dokunmaz; kesinti yalnız bugünkü tek-replika rollout'unun kendi davranışıdır (`20-app.yaml:53-66`).
