# Backlog — kullanıcı eylemi bekleyen işler

> **Bu dosya yalnız KULLANICININ yapabileceği işleri tutar** — fiziksel cihaz gerektiren
> ölçümler, hesap/abonelik kararları, sudo isteyen kurulumlar, dış servis kayıtları.
> Ajan bunları kodla kapatamaz.
>
> **Bu bir durum dosyası DEĞİL.** Projenin canlı durumu tek yerdedir:
> [docs/plan/state.md](plan/state.md) (CLAUDE.md §12). Buraya görev durumu, ledger
> satırı veya "sıradaki iş" yazma — çelişirler. Buradaki bir madde bir görevi
> bloklamıyorsa bunu açıkça söyler.
>
> Kullanıcı **"backlog ekle"** dediğinde madde buraya eklenir.

---

## Açık

### B1 — iOS Safari çerez ömrü ölçümü (Q11)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q11, M5-01 (`a71e1b2`), M8-05

**Ne yapılacak.** Gerçek bir iPhone'da uçtan uca çerez ömrünü ölç:

1. Plakete dokun → Safari açılır → aktive ol (çerez yazılır).
2. **Günler/haftalar** bekle — arada Safari'yi normal kullan (ITP'nin tetiklenmesi için).
3. Aynı telefonla tekrar dokun → **hâlâ tanınıyor musun?**

**Neden önemli.** Ürünün "telefon seni tanır" vaadi buna dayanıyor. Çalışan uygulama
kurmuyor; tek kimlik kanıtı bu kalıcı çerez (§5'in "kim" kanıtı).

**Teknik olarak sınanan şey.** Safari ITP, **JavaScript ile yazılan** çerezleri 7 güne
kırpar. **Sunucunun `Set-Cookie` ile yazdığı `httpOnly` çerez** bu kırpmaya tabi
*olmamalı* — M5-01 tam olarak öyle yazıyor (`httpOnly`, `SameSite=Lax`,
`Max-Age=31536000`). Ölçüm bu ayrımın gerçekte tuttuğunu doğrular.

**M5-01'i bloklamadı, çünkü:** sunucu tarafı sonuçtan bağımsız — ölçüm ne çıkarsa
çıksın kod aynı. Bu yüzden bir kabul kriteri olamaz (kart düzeltmesi, 2026-07-31).
Sonuç çıkınca: [open-questions.md](plan/open-questions.md) → Q11'e yaz.

**Sonuç kötü çıkarsa** (çerez erken düşüyorsa) seçenekler: aktivasyonu daha sık
tekrarlatan bir akış, ya da çereze ek olarak ikinci bir tanıma kanıtı — **ama §4.1
gereği biyometri/WebAuthn/attestation değil.** Karar o zaman verilir.

### B3 — Saklama süresinin hukuki onayı (Q13)

**Durum:** açık · **Bloklar:** hiçbir şeyi (kod hazır) · **İlgili:** Q13, M5-02, M8-06

**Ne yapılacak.** `TAPPA_RETENTION_YEARS`'in **gerçek** değerini bir hukukçuya doğrulat
(Malta/AB istihdam + bordro/vergi saklama yükümlülüğü) ve `.env`'e yaz.

**Neden önemli.** Bu sayı **GDPR Art. 13 aydınlatma metninde çalışana gösteriliyor** —
yani hukuki bir beyan. Yanlışsa metin çalışana yanlış bilgi vermiş olur.

**Kod tarafı hazır ve bu yüzden bloklamıyor** (kullanıcı kararı 2026-07-31): sayı
**koda gömülmedi**, config'den geliyor; aydınlatma metni onu render ediyor. Dev/demo
değeri bir **varsayılan**dır, hukuki iddia değildir — kod bunu böyle söylüyor.
Doğru değer öğrenilince tek yapılacak `.env`'i güncellemek; sürüm çıkmaya gerek yok.

**Sonuç çıkınca:** [open-questions.md](plan/open-questions.md) → Q13'e yaz ve buradaki
maddeyi "Kapananlar"a taşı. Q13 ayrıca "GDPR silme talebi × immutable `transactions`"
sorusunu da taşıyor (M8-06) — saklama süresi onun bir parçası, tamamı değil.

### B2 — arm64 Go toolchain kurulumu (Q26)

**Durum:** açık · **Bloklar:** hiçbir şeyi · **İlgili:** Q26, M0-07

**Ne yapılacak.** Yerel Go toolchain'i arm64'e geçir. Tarball indirildi ve checksum'ı
go.dev ile **birebir doğrulandı** (`efb87ff2…`), ama `/usr/local`'a kurulum **sudo
parolası** istiyor — kullanıcı çalıştırmalı. Komutlar
[state.md](plan/state.md) oturum notunda.

**Kazanç yalnız hız.** Her şey bugün amd64 Go 1.26.5 ile yeşil; Rosetta altında derleme
~2-3x yavaş. Kurulunca `go version` → `darwin/arm64` olur ve ilk `make gen` bir kez
uzun sürer (build cache + pinli CLI önbellekleri tazelenir — bozukluk değil).

---

## Ertelenmiş teknik borç (sahibi bir görev kartı DEĞİL)

> Bunlar **kullanıcı eylemi değil**, ama hiçbir yakın görev kartının da sahiplenmediği işler —
> çoğu dağıtım (M8) zamanına ait. Buraya yazılmalarının sebebi kaybolmaları: M8 uzak.
>
> ⚠️ **Görev-kapsamlı devirler buraya YAZILMAZ** — onlar [state.md](plan/state.md)'deki
> "M5-04/M5-05'e devralınan" gibi bölümlerde ve sahibi bellidir (ör. 429'un §4.6 kalıntısı → M5-04,
> N5 tenant-mismatch → M5-05, font self-host → M5-04). İki yere yazılan gerçek çelişir.

| # | Ne | Nereden | Sahibi |
|---|---|---|---|
| **T1** | **Oran sınırlayıcı süreç-içi ve sabit-pencere.** İki instance sınırı ikiye katlar; pencere sınırında kısa sürede 2×limit mümkün; `limiterMaxKeys` (100k) aşılınca map **toptan sıfırlanıyor** (fail-open, bilinçli ve yazılı). Paylaşılan store gerekir. | M5-02, M5-03 | M8 |
| **T2** | **`TAPPA_TRUSTED_PROXIES` dağıtım kontrol listesi.** Proxy XFF'e **append** etmeli — **replace** eden proxy koddan ayırt **edilemez**. Kapı yalnız tek girdilik `/0`'ı yakalar; `/1` veya birleşimle tüm uzayı kaplayan liste (`0.0.0.0/1,128.0.0.0/1`) yakalanmaz. Güvenilen aralık **gerçek istemcileri içeriyorsa** onlar serbestçe IP uydurabilir (ölçüldü). | M5-03 | M8 |
| **T3** | **Çerez gölgeleme + `__Host-` öneki.** Aynı isimli iki `tappa_activation` çerezinde `r.Cookie` **ilkini** alıyor; alt alan adı kontrolü gerektirir. | M5-02 | M8 |
| **T4** | **`docker-compose.yml` `log_statement=all` bind parametrelerini log'a yazıyor** (denetçi 30 dk'da `$1 = '<64-hex>'` biçiminde **1036 satır** saydı). `code_hash` taşıyıcı kimlik bilgisi olduğu için dev Postgres log'u bir aktivasyon anahtarı deposuna dönüşüyor. **Dev-only** ve denetçiler bu log'u ölçüm aracı olarak kullanıyor → bilinçli bırakıldı; üretim deploy config'i repoda henüz yok. | M5-02 | M8 |
| **T5** | **`config.Load` `TAPPA_BASE_URL`'ü doğrulamıyor.** `NewCookies` bir **prefix testi** yapar, URL parse etmez → başında boşluk olan veya URL olmayan değer NOT-Secure dalına düşer (**non-prod'la sınırlı**; prod koşulsuz Secure). | M5-01, M5-02 | M5-03 sonrası / M8 |
| **T6** | **Dev-DB test kalıntısı birikiyor.** `tenants` ~7250 satır; `audit_log`/`transactions` append-only olduğu için **tasarımca** temizlenemez (M1-09: imkânsızlık = garanti). Kırmızı çizgi değil, hijyen. Demo/prod öncesi `make db-reset`; kalıcı çözüm testcontainers ile izole DB. | M3-02, M5-03 | M8 |
| **T7** | **`aes_key_ref` KEK zarfı ŞEMA TARAFINDAN ZORLANMIYOR — ve M6-06 B ona bir EKRAN YÜZÜ verdi.** Seed yarısı M5-09'da kapandı (gerçek 44 baytlık zarf + drift guard). Açık kalan: şema `bytea`ya *"bu bir KEK zarfıdır"* dedirtemiyor, yani **insert yolu** disipline bağlı. ⚠️ **YENİ (güvenlik merceği, 2026-08-10):** panelin ***"Encoded by Tappa"*** etiketi `location_id IS NULL`'a bakıyor, **anahtara DEĞİL**. Ölçüldü: 2 baytlık bir `aes_key_ref` taşıyan satır kartta **"Encoded by Tappa"** gösteriyor, ve o plakete NFC tap → **500** ve `transactions`'a **hiçbir satır yazılmıyor** (sayaç 1→1) — **o yolda bir §4.6 deliği**. Panelin kusuru **değil** (panel öyle bir satır yaratamaz; `tags` INSERT'i yok → **T16**), ama ekran artık **doğrulamadığı bir şeyi adlandırıyor**. Dağılım: **44 → 55.689 · 2 → 19.783 · diğer → 3.873**; `CHECK (octet_length = 44)` bugün **21.985** satırı ihlal eder → yine `NOT VALID` + yedi fixture'ın gerçek zarfa çevrilmesi. Üç seçenek M5-09 kartında. | M1-05, M5-09, M6-06 B | M8-05 (plaket yükleyici) |
| **T8** | **🟡 BÜYÜK ÖLÇÜDE KAPANDI — 00013 (`ba671b0`, 2026-08-09).** `CHECK (uid ~ '^[0-9A-F]{14}$')` sevk edildi; küçük harfli INSERT **reddediliyor**, iki-yazım/tek-AAD sınıfı **yeni satırlar için kapalı**. 🔴 **AMA BU MADDENİN ÖNCÜLÜ YANLIŞTI VE ÖLÇÜMLE ÇÜRÜTÜLDÜ:** *"mevcut 12 satır zaten büyük harf → veri taşıması yok"* — gerçek **18.010 küçük harfli** satır, ve **12.437'si `transactions`'ın 24.874 satırından** referanslı; `transactions` **DB düzeyinde append-only** (tetikleyici `tappa_owner`'ı da bağlıyor) → normalleştirme **§4.3 ihlali** olurdu. Bu yüzden kısıt **`NOT VALID`**: artık satırlar **donuyor** (her UPDATE 23514) ve **tap yolu onlara ULAŞAMIYOR** (`sun.Parse` daima büyük harf üretir, `char(14)` karşılaştırması harfe duyarlı — güvenlik merceği ayrıca **ulaşan yol aradı, bulamadı**). Kirlenmenin **kaynağı kapatıldı** (`ToUpper`'sız iki test yardımcısı; iki tam koşumdan sonra sayı **artmadı**). **KALAN İŞ → T15.** | M5-09 · 00013'te kapatıldı | — |
| **T9** | **✅ KAPANDI — 00013 (`ba671b0`, 2026-08-09).** `REVOKE UPDATE ON tags` + `GRANT UPDATE (location_id, last_ctr, status, retired_at, replaced_by)` + monotonluk trigger'ı (`tappa_owner`'ı da bağlıyor). Ölçüldü: `aes_key_ref` ve `uid` UPDATE'i → **42501**, rewind → **23001** (her iki rol için), advance ve `last_ctr`'a dokunmayan retire → **UPDATE 1**. 🔴 **BU MADDENİN İKİ CÜMLESİ DE YANLIŞTI:** (a) önerilen trigger koşulu `NEW.last_ctr > OLD.last_ctr` **`last_ctr`'a dokunmayan bir retire'ı REDDEDİYOR** (canlı hata mesajıyla gösterildi) — doğrusu `< OLD` ise `RAISE`; (b) sütun listesinde **`location_id` yoktu**, ama envanter modeli **bağlamayı** istiyor ve bağlama `location_id` UPDATE'idir → *aynı günün iki kullanıcı kararı birbirini uygulanamaz kılıyordu*. **Ders: bir borç maddesinin önerdiği ÇÖZÜM de, öncülü kadar ölçülmelidir.** | M5-09 · 00013'te kapatıldı | — |
| **T10** | **🔴 CI OLDUĞU GİBİ KIRMIZI VERİR VE BUGÜNE KADAR KİMSE GÖRMEDİ.** Repoda **uzak yok** (kullanıcı kararı: push/PR yok) → `ci.yml` **hiç çalışmadı**; "CI yeşil" teorik bir cümle. Olduğu gibi koşarsa: workflow `make up` koşuyor (yalnız Postgres'i **başlatır**) ama **`make migrate` koşmuyor**, `make seed` koşmuyor, `TAPPA_TAG_KEK` **vermiyor** → `make check` → `make test` migrate edilmemiş şemaya çarpar. Ölçüldü (boş DB): **17 pakette 140 üst düzey FAIL**, 136'sı M5-09'dan **eski** (M1-09'dan beri taşınıyor). Düzeltme tek satır değil (migrate + KEK + seed) ve **uzak olmadan doğrulanamaz**. O zamana kadar `make check`'in tek gerçek koşum yeri geliştiricinin makinesidir. | M5-09 güvenlik denetimi | M8 / M0-06 |
| **T11** | **✅ KAPANDI — 00013 (`ba671b0`, 2026-08-09).** `transactions(tenant_id, location_id)` eklendi: dört sayım **indekssiz 30–75 ms → indeksli 5–9 ms**, `transactions` bacağı **Bitmap Heap Scan → Index Only Scan**; bedel **~4,5 MB**, yazma maliyeti **üç eşleştirilmiş 20.000-INSERT örneğiyle** ölçüldü, anlamlı yavaşlama yok. ⚠️ Beklenmedik ikinci kazanç: `ListTagLastSeen` de bu indeksi **tenant filtresi** olarak kullanıyor (düşünce plan `transactions_tenant_occurred_idx`'e düşüp **69.575 indeks satırı** okuyor). ⚠️ **Bu 00013'ün tek performans parçası ve hiçbir test onsuz kırılmıyor** — migration'da yazılı. | M6-06 A · 00013'te kapatıldı | — |
| **T12** | **MAC'li onay jetonu TEK KULLANIMLIK DEĞİL.** Aynı jetonla ikinci POST yine geçiyor (ölçüldü; bir denetim tek mint'i **üç kez** harcadı). Bugün zararsız, çünkü her kapının **kendi** ikinci savunması var: deaktivasyonda `status <> 'deactivated'`, kaldırmalarda **hiçbir satırla eşleşmeyen ikinci DELETE**. ⚠️ Bu, M6-06 A'da *"silme onayını imzala"* seçeneğinin **elenme gerekçesiydi** — imzalı bir iddiayı TTL içinde yeniden harcanabilir kılmak kusuru kapatmaz, **taşır**. Kapatmak harcanmış-değer tablosu **artı kendi saklama hikâyesini** ister → ayrı görev. `deactivateconfirm.go` bunu 12 parçalık listesinde **açık limit** olarak sayıyor. | M6-05 B, M6-06 A | M7 |
| **T13** | **`tappa_owner` `audit_log`'u TRUNCATE edebiliyor.** Append-only tetikleyici `BEFORE UPDATE OR DELETE` — **TRUNCATE'i kapsamıyor** (`tappa_app` edemiyor: yalnız SELECT+INSERT). 00005'in durumu, bu fazın değil; **ama bedeli M6-06 A ile değişti**: `audit_log` artık silinmiş bir lokasyonun **tek hayatta kalan kaydı** ve C′ silme bildiriminin **tek dayanağı**. Bir truncate onay **uydurmaz** — hepsini **susturur** (ekran zaten "hiçbir şey söyleme" yönüne düşüyor). | M5-09, M6-06 A güvenlik denetimi | M7/M8 |
| **T14** | **Onay jetonunu ÜRETEN GET, Origin kapısının dışında.** `?venue=<id>&confirm=remove` çapraz-origin bir üst düzey gezinmede jetonu basıp `Set-Cookie` yazıyor (`tappa_admin_confirm`, HttpOnly, SameSite=Lax, Path=/admin). Saldırgan gövdeyi **okuyamaz** (jetonu öğrenemez) ve POST Origin kapılı; kalan risk yöneticiye istemediği bir *"Remove X?"* uyarısını **gösterebilmek**. **Bu faza özgü değil** — M6-05 `employees.go` aynı şekli sevk etmişti. | M6-05 B, M6-06 A güvenlik denetimi | M7 |
| **T15** | **`tags_uid_canonical_hex` HÂLÂ `NOT VALID` — ve bunu tamamlamak veri temizliği ister.** 00013 kısıtı `NOT VALID` sevk etti (gerekçe T8'de: normalleştirme §4.3'e çarpıyor). Yeni satırlar **denetleniyor**, artık **18.010** küçük harfli satır **donmuş** durumda ve tap yolundan **ulaşılamıyor**. Tamamlayan tek satır: `ALTER TABLE tags VALIDATE CONSTRAINT tags_uid_canonical_hex;` — ama önce o satırların gitmesi gerekiyor, yani **`make db-reset`** (backlog **T6**'nın zaten istediği şey). ⚠️ **Üretimde sorun yok:** taze migrate+seed bir DB'de ihlal eden **0** satır var ve `VALIDATE` **başarıyla koşuyor** (güvenlik merceği ölçtü) — yani üretim kısıtı **fiilen validated doğar**. Bu bir **dev-DB hijyeni**, pilot öncesi. | M6-06 B veri katmanı | M8 (pilot öncesi) |
| **T16** | **`tappa_app` `tags` üzerinde TABLO GENELİNDE INSERT tutuyor — 00013'ün *"dürüst zayıf noktası"*.** Ölçüldü: `INSERT INTO tags (…, '\xdead', 'unassigned')` kendi tenant'ında **INSERT 0 1** (başka tenant'a RLS `WITH CHECK` reddediyor). Yani bir SQL enjeksiyonu ya da gelecekte yazılacak bir yükleyici sorgusu, **kendi tenant'ında** hem bir fail-open durum satırı hem **bozuk bir `aes_key_ref` zarfı** üretebilir — ikisi tek yetkiyle bitişik. **Bugün ulaşan sorgu yok** (`db/queries`'de `tags`'a **sıfır** INSERT; üretim Go'sunda sıfır; dinamik SQL yok). ⚠️ Bu, **T8'in ve fail-open guardrail'in *bugün sömürülemiyor* korumasının dayandığı tek şey** — ve o koruma bir **yükleyici yazıldığı gün** biter. Çözüm: plaket yükleme **`tappa_owner`'ın işi** (M8-05 runbook) + `REVOKE INSERT ON tags FROM tappa_app`. ⚠️ Bugün **7 test dosyası** o INSERT'i kullanıyor. | M6-06 B güvenlik denetimi | **M8-05'ten ÖNCE** |
| **T17** | **`audit_log`'da `(tenant_id, target)` indeksi yok ve `ListPlaqueHistory` tenant'ın TÜM audit hacmini tarıyor.** Güvenlik merceğinin `EXPLAIN ANALYZE`'ı (2026-08-10, geçmişi **0 satır** olan bir plaket): `Bitmap Heap Scan on audit_log` · **Rows Removed by Filter: 2.566** · `Heap Blocks: exact=1039` · `shared hit=1064` · **2,469 ms** — **dönen satır 0**. Yani maliyet plaketin geçmişiyle değil **tenant'ın toplam audit hacmiyle** büyüyor, ve `audit_log` **append-only**, retention job **yok**. Bir oturum bütçesi (300 istek / 10 dk) kadar kart açmak bunu 300 kez ödetir. ⚠️ Sorgunun yorumu *"BOUNDED BY @row_limit"* diyordu — **sınırlı olan ÇIKTI, tarama değil**; cümle M6-06 B kapanışında düzeltildi. Çözüm `CREATE INDEX ON audit_log (tenant_id, target)` (ya da `(tenant_id, action, target)`) — **migration ister**; 00013 M6-06 B'nin yuvasıydı ve harcandı. **M6-07 (raporlar) da audit indeksleri isteyecek → birlikte yapılmaları ucuz.** | M6-06 B güvenlik denetimi | M6-07 / M7 |
| **T18** | **Toplu CSV çıkışı audit yazımı düşerse İZSİZ gider.** `internal/handler/reportscsv.go` `a.record`'un dönüş değerini okumuyor; indirme koşulsuz devam ediyor. Yapıcının gerekçesi *"aynı sayılar `GET /admin/reports`'ta zaten audit'siz erişilebilir"*di ve **üçte biri yanlış**: `maxReportRows = 100`, `maxOpenRows = 100`, ve ekranda **hiç kursör yok** (`reports.go:66` kendi ağzıyla *"this page cannot page"* diyor) → **101+. kişinin satırları YALNIZCA export'tan** alınabiliyor. Fark **gizlilik değil hesap verebilirlik**: alttaki kayıtlar sayfalanabilir Transactions bölümünden okunabiliyor ve o da audit yazmıyor, yani **§4.6 ihlali değil** (kaybolan bir *kayıt* yok). Pencere dar (rapor okuması başarılı + aynı Postgres'e audit INSERT'i başarısız) ve hata `slog.Error` ile bağırıyor. Kapatmak *"audit yazamazsam indirme yok"* demek ister — **bir ürün kararı**, GDPR tarafıyla birlikte. | M6-07 B güvenlik denetimi | M7 |
| **T19** | **Çapraz-origin bir gezinme, SİLİNEMEYEN bir `audit_log` satırı yazdırabiliyor — T14'ün daha AĞIR üyesi.** `GET /admin/reports.csv` `mountSections`'da, yani `sameOriginGate`'siz; **bilinçli** (gate `Sec-Fetch-Site: none` → **red** verdiği için **yer iminden/adres çubuğundan** açılan indirmeyi kırardı — ölçüldü, *"eski tarayıcılar"* değil **her tarayıcı**). Ölçüldü: `Origin: https://evil.example` + `Sec-Fetch-Site: cross-site` + canlı çerez → **200 + 1 audit satırı**. **Saldırgan dosyayı OKUYAMIYOR** (`Access-Control-Allow-Origin` **yok**; çerez `SameSite=Lax`). Kalan zarar **kalıcı iz kirliliği**: T14'ün yan etkisi bir **çerez** (istemci tarafı, kendi kendini sınırlar), buradaki **append-only bir tablodaki satır** ve §4.3 tetikleyicisi yüzünden **kimse silemez**. Sınırlar: `floodGate` + `sessionGate` ücretlendiriyor (301. istek **429**), satır aktörü **adlandırıyor**. T14 ile birlikte bakılmalı. | M6-07 B güvenlik denetimi | M7 |
| **T20** | **Türetilmiş §4.5 kuşak ağının `INSERT … VALUES` KÖR NOKTASI — ve ağ iki bağımsız yoldan yenildi.** M6-08'de belt `transactions.sql`'e genişletildi ve denetçinin gevşetmesini (`OR e.tenant_id IS NOT NULL`) **yakaladı**. Ama ölçüldü: ürünün **70 sorgusundan 12'si INSERT, 7'si `INSERT … VALUES`** ve eşleştirici onları **yapısal olarak göremiyor** — `RecordAuditEvent` · `CreateInvite` · `EnsureBaselinePolicy` · `EnsureBaselinePolicyVersion` · `EnsurePolicyAttachment` · `CreateSession` · **`InsertTransaction`** (ürünün ana yazma yolu). ⚠️ **İki kaçış daha kuruldu ve ikisi de işledi:** (i) satırın `tenant_id`'sini **ikinci bir parametreden** yazmak — eşleştirici SELECT'in `WHERE`'ine bakıyor, INSERT'ün **yazdığı** değere **hiç bakmıyor**; (ii) kapsamsız bir okumayı **işaret edilmemiş bir dosyaya** koymak (`reviews.sql`) → üç ağ da görmedi. **Üçü de RLS ile kapalı** (`WITH CHECK` 42501, composite FK 23503), yani **belt deliği, canlı açık değil**. Anti-vacuity muhafızı var: `transactions.sql`'e yeni bir `INSERT … VALUES` eklemek testi **kırmızı** yapıyor, yani sessiz değil. | M6-08 doğrulama denetimi | M7 |
| **T21** | **`problemPanelUnavailable` yazma yollarında yanlış cümle kuruyor — 13+5 çevrim SAHİPSİZ.** Metin *"We could not **read** your records … this page is **not showing anything**"* diyor; müdür **yazıyordu**. (Son cümlesi — *"no record has been lost"* — güvenlik merceğince **doğrulandı**: INSERT patlarsa transaction geri alınıyor.) Ölçüldü: **32 çağrı yeri**, **14'ü `mountWriting` handler'larında**, 5'i daha yeniden-render yardımcılarında. M6-08 `problemPanelWriteFailed`'i ekledi ve **yalnız kendi 3 yerini** çevirdi. Kalan **13+5** nominal olarak M6-04/M6-05/M6-06'nın ama **üçü de `done`** → sahipsiz. İkinci temsil riski **düşük** (iki cümle ayrı olguları anlatıyor). | M6-08 denetimleri | M7 |
| **T22** | **`make db-reset` KIRIK — ve bu T6/T15'in beklediği tek temizlik yolu.** Ölçüldü: `00013`'ün `Down`'ı biriken geliştirme verisine takılıyor — önce `tags_status_check` (**30 `unassigned` satır**), sonra `location_id NOT NULL` (**34 satır**). ⚠️ **Yeni bulgu DEĞİL:** `00013`'ün kendi `Down` başlığı bunu **2026-08-09'da** yazmış; yeni olan, artık **fiilen kullanılamaz** olması. Bedeli M6-09'da somutlaştı: `policies` **DELETE vermiyor** (§4.6, bilinçli) ve `policy_versions` sahibe bile kapalı, yani mutasyon/prob koşularının bıraktığı satırlar **hiçbir ürün yoluyla silinemiyor** — taban `DROP SCHEMA public CASCADE` + db-init'in extension/grant'ları + `make migrate seed` + **`make simulate-day`** ile **elle** kuruldu (seed policy satırı yazmıyor; ilk tap materialise ediyor). ⚠️ Elle kurulan taban **eksiksiz çıktı** (güvenlik merceği doğruladı: `tappa_app` NOBYPASSRLS, 7 tabloda FORCE RLS, grant'lar tam) ama **bu bir prosedür değil, bir kurtarma**. Pilot öncesi ya `00013`'ün `Down`'ı veri-toleranslı yazılmalı ya `db-reset` migration'lardan bağımsız (drop+recreate) olmalı. | M6-09 B, 5.–9. tur | M8 (pilot öncesi) |
| **T23** | **🔴 SEVK EDİLMİŞ BİR BASELINE KURALININ GEREKÇESİ YANLIŞ — VE MÜŞTERİYE BASILIYOR.** `internal/policy/baseline.go:208`, `base:gps-conflict-review`'ın `Reason`'ı: *"**IP matched but** GPS places the device away from the location"*. **M6-11 bunun yanlış olduğunu ölçtü:** `decide.go:715-722` `conflict = !match` döndürüyor — **yalnız mesafeden**, adrese **hiç bakmadan** — ve `baseline.go:206-208`'in koşulu yalnız `{CtxTapGPSConflict: true}`, yani **politika da daraltmıyor**. Cümle `/admin/policies` render'ında **birebir basılıyor** (`policies.templ:448` ← `policies.go:505`), yani müşteri **var olmayan bir mekanizmayı** okuyor. ⚠️ **Neden ayrı görev:** sevk edilmiş bir baseline **belgesinin gövdesini** değiştirmek materialise/sürüm sorusu açar (`EnsureBaselinePolicyVersion` yeni sürüm yazar mı, yazan tenant'larda ne olur) ve `policy_versions` **append-only**. M6-11 kartı düzeltildi; **ürün düzeltilmedi**. | M6-11, 1. tur (genel göz N10) | M7-03 ya da ayrı |
| **T24** | **`FILTER (WHERE …)` kör noktası §4.5 ağının İKİ kardeş kopyasında duruyor.** M6-11 `internal/domain/ledger/query_test.go`'yu düzeltti (kör nokta **gerçekti**: daraltma kaldırılınca doğru SQL üzerinde **28 yanlış alarm**). `internal/domain/review/query_test.go` ve `internal/domain/tenant/query_test.go` **düzeltilmedi** (`grep -c filterRE` → 0, 0). ⚠️ **Bugün ATIL ve ölçüldü:** `db/queries/*.sql` içinde `anomalies.sql` dışında `FILTER (` **0 adet**, yani o iki paketin çağırdığı hiçbir sorgu bu şekli kullanmıyor. Sapma `ledger` tarafında **açıkça yazılı**. Riski: o paketlerden birine agregat `FILTER` taşıyan bir sorgu girdiği gün ağ **yanlış alarm** verir (fail-safe yön) ya da daraltma kopyalanmadan eklenirse **sessiz kalır**. | M6-11 doğrulama denetimi | M7 |
| **T25** | **⚠️ ÜRÜN KARARI: `ListTapsTakenTogether` YENİ BİR SOSYAL ÇIKARIM YETENEĞİ.** Ekran *"her gün saniyeler içinde birlikte tap yapan kişiler"*i listeliyor — kim kiminle, ne sıklıkta, kaç saniye arayla. **`tappa-security-auditor` §4 açısından TEMİZ buldu** ve kartın *"rapor yorum yapmaz"* ilkesine uyuluyor (sayfa *"Two people arriving together is not a finding … colleagues who share a lift produce exactly this row"* diye açılıyor, eşikler **20 sn / 2 ayrı gün** ekranda). **Ama bu, ürünün daha önce yapamadığı bir çıkarım** ve güvenlik merceğinin kendi ifadesiyle *"ürün sahibinin bilerek onaylaması gereken bir yetenek"*. Karar: kalsın mı, eşikler değişsin mi, yoksa müdür ekranından çıkıp yalnız denetim izine mi düşsün? | M6-11 güvenlik denetimi | kullanıcı kararı |

---

## Kapananlar

*(henüz yok)*
