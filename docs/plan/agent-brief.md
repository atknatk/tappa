# Ajan brief'leri — yapıcı ve üçüncü göz

Bu dosya [README.md → Çalışma modu](README.md)'nun uygulama detayıdır: alt
ajanlara verilecek görev tarifinin **şekli** ve her turda tekrarlanan kurallar.

**Neden yazılı:** brief'in şekli ilk turlarda sohbette taşınıyordu. Bağlam
sıkıştırılınca kaybolur ve disiplin sessizce gevşer — kalıcı olmak zorunda.

---

## Sabit kurallar — her yapıcı brief'inde tekrarlanır

Bunlar kartın içeriğinden bağımsız, her görev için geçerli:

1. **Önce oku:** `CLAUDE.md` (ilgili bölümler + §4 kırmızı çizgiler) · görev
   kartı · kartın atıf yaptığı dosyalar. Kart bir skill/agent gösteriyorsa onu da.
2. **Sır yazdırma.** Anahtar, token, CMAC, davet kodu, tam GPS — ne çıktıya, ne
   log'a, ne dosyaya. Doğrulama değeri göstermeden yapılır (uzunluk, hash öneki,
   eşitlik boolean'ı).
3. **`docs/plan/state.md` ve `roadmap.md`'ye dokunma** — orkestratörün.
4. **Commit atma** — orkestratörün.
5. **Süreç hijyeni:** sunucu başlattıysan **süreç grubunu** öldür (`set -m` +
   `kill -- -$PID`) ve **portla** doğrula (`lsof -nP -iTCP:8080 -sTCP:LISTEN`).
   Süreç **adı desenine güvenme** — `go run` bir sarmalayıcıdır, gerçek ikili
   build cache'tedir ve yolunda `exe/` **geçmez**.
6. **Kart gerçekle çelişiyorsa kartı düzelt**, gerekçesini görünür bir blokla yaz:
   `> **Kart düzeltmesi (TARİH, MX-YY uygulaması sırasında).**` — biçim örneği
   M0-01, M0-02, M0-04 kartlarında.
7. **Başarısızlığı gizleme.** Bir adım patladıysa patladığını söyle; "geçmiş
   gibi" raporlama denetimde çıkar ve tur tekrar edilir.
8. **Kapsam dışına çıkarsan açıkça işaretle** ve gerekçesini yaz. Sessiz genişleme
   yasak; gerekçeli genişleme kabul edilebilir.
9. **Teslim:** her komutun **gerçek** çıktısı · kabul kriterlerinin her biri için
   kanıt · `git status --short` ile kapsam · sapmalar.

## Sabit kurallar — her denetçi brief'inde tekrarlanır

1. **Denetçi, işi yapan ajan olamaz.** Kendi işini denetleyen ajan kendi
   varsayımlarını doğrular.
2. **Hiçbir dosyayı değiştirme.** Tek istisna: kendi başlattığın süreçleri
   temizle, portu boş bırak; sondajını repo **dışında** yap ve sil.
3. **Yapıcının raporuna güvenme.** Her ölçülebilir iddiayı **kendi komutunla**
   yeniden üret.
4. **`exit 0` kanıt değildir.** Üretilen kod derleniyor mu (`go build`, `go vet`)?
   Araç "başarılı" deyip kullanılamaz çıktı verebilir — bu vaka gerçekten yaşandı
   (`sqlc.yaml` `inet` override'ı).
5. **Sondaj asgari değil, kapsayıcı olsun.** Bir config'i doğrularken **tüm**
   yollarını tetikle. Asgari örnekle sınamak, yapıcının M0-04'te üç bozuk
   override'dan ikisini kaçırmasının sebebiydi.
6. Her bulgu: **ne · nerede (`dosya:satır` veya komut) · somut sonucu ne.**
7. **Övgü ve özet yazma.** Emin olamadığını "temiz" sayma — "doğrulanamadı" de.
8. Kapsam dışı gerçek sorun görürsen yaz, ama görevi **bloklayıp bloklamadığını
   açıkça ayır**.
9. **Çıktının son satırı** tam olarak şu iki formattan biri:
   `VERDICT: ONAY` · `VERDICT: RED — <tek cümlede en ağır bulgu>`

---

## Yapıcı brief şablonu

```
Tappa projesinde **<ID>** görevini uygula. Repo: <yol> · dal: <dal>

## Önce oku (zorunlu)
1. CLAUDE.md — <ilgili bölümler> + §4
2. docs/plan/<milestone dosyası> → <ID> kartı
3. docs/plan/agent-brief.md → "Sabit kurallar — yapıcı"
4. <kartın atıf yaptığı dosyalar>

## Görev
<kartın özeti — kartı kopyalama, ajan zaten okuyacak>

## Bu göreve özel kurallar
<kartın tuzakları + bu turda geçerli yasaklar/yetkiler>

## Doğrulama
<kartın kabul kriterleri + ek regresyon: go build, go vet, redline-check>

## Teslim
<sabit kurallar madde 9>

Not: Ardından bağımsız bir denetçi ajan her şeyi baştan doğrulayacak.
```

## Denetçi brief şablonu

```
Sen Tappa projesinde ÜÇÜNCÜ GÖZ DENETÇİSİSİN. Repo: <yol> · dal: <dal>

<ID> görevi <n>. turda. <önceki turlarda ne bulundu>.

## Önce oku
1. docs/plan/<milestone> → <ID> kartı (bu turda değiştiyse söyle)
2. docs/plan/agent-brief.md → "Sabit kurallar — denetçi"
3. CLAUDE.md §4 + <ilgili bölümler>
4. <ilgili dosyalar>

## Yapıcı ajanın iddiaları
<madde madde, ölçülebilir biçimde>

## Denetim listesi
<kabul kriterleri tek tek + kapsam + regresyon + süreç/port + sızıntı>

## Çıktı biçimi — zorunlu
<sabit kurallar madde 9>
```

---

## Orkestrasyon ritmi — M5'te fiilen uygulanan tur döngüsü

> **Neden yazılı:** [README.md](README.md) *ne* yapılacağını söylüyor (yapıcı → üçüncü göz → onay).
> Bu bölüm M5-01…M5-05'te **fiilen işleyen** ritmi kaydeder. Beş görevde **9 RED** çıkardı; ritim
> gevşerse o RED'ler kaçar. Bağlam sıkıştırılınca bu bilgi sohbette kalmaz — kalıcı olmak zorunda.

**Bir görevin turu:**
1. **Orkestratör kartı ve devirleri okur**, brief'i yazar. Kart gerçekle çelişebileceği için brief
   *"çelişirse kartı düzelt"* der. **Bilinen tuzakları brief'e önceden koy** — M5-05'te "baseline
   materyalize değil, ölç" uyarısı bloklayanı ilk turda buldurdu.
2. **Yapıcı** (model `opus`) uygular. Brief her zaman: sabit kurallar · kırmızı çizgiler · **ölçüm
   isteyen** doğrulama listesi · mutasyon + pozitif kontrol zorunluluğu.
3. **Üçüncü göz** (model `opus`, **her turda YENİ ajan**) denetler. Brief'e **yapıcının iddiaları
   madde madde** yazılır ki denetçi *her birini kendi komutuyla yeniden üretsin*. "Raporuna güvenme"
   yeterli değil — iddiayı önüne koy.
4. **Kırmızı çizgiye değen her işte** ayrıca `tappa-security-auditor` (**yeni örnek**). Farklı mercek:
   genel denetçi doğruluk, güvenlik denetçisi §4. **Biri diğerinin yerine geçmez** — M2-04'te güvenlik
   denetçisi "temiz" derken genel göz byte-reversal buldu; M5-03'te tersi oldu.
5. **RED → düzelt → YENİ denetçi.** Onay gelmeden `done` yok, commit yok.
6. **Ucuz bloklamayan bulguları commit'ten ÖNCE kapat.** M5-01'de dördü kapatılmasa aynı sınıf
   M5-02'de tekrar çıkacaktı. Devretmek yalnız gerçekten başka görevin işiyse.
7. Orkestratör **kendi doğrular** (`make check`, ağaç, kritik iddialardan birkaçı), commit'ler,
   `state.md` + gerekirse `agent-brief.md`'yi günceller.

**Ne zaman durulur — M5-06 ve M5-08'den çıkan iki durma kuralı:**
- **Kod yakınsadığının işareti: bulguların METNE dönmesi.** M5-06 15 tur, M5-08 8 tur sürdü; ikisinde de
  son üç tur **yalnız cümle/sayı** düzeltti. Bulgular hâlâ davranış değiştiriyorsa dur**ma**; art arda
  iki tur yalnız metin çıkarıyorsa mekanizma oturmuştur, kalanı belgeyi ölçüme eşitlemektir.
- **Yeni kanal KAPATILMAZ, sayılır** (M5-06'nın 11. turunda kondu, M5-08'de de işe yaradı). Bir noktadan
  sonra her koruma bir sonraki turda yeniliyorsa: kapatmayı bırak, **dürüstçe LİMİT olarak yaz**. Sayılmış
  bir açık, kapatıldığı **iddia edilen** bir açıktan güvenlidir. Ve hiçbir yere *"tamamen / bitmiş /
  complete / exhaustive"* yazma — yazmadan önce **yenmeye çalış**, yenemediysen **nasıl denediğini** yaz.

**Kullanıcıya ne zaman sorulur — 5. oturumda ON kez soruldu, onu da işin şeklini değiştirdi:**
marka token'ı/kontrast (§9) · ekran metninin var olmayan bir şeyi vaat etmesi · **kartın CLAUDE.md ile
çelişmesi** (kart mı yanlış, §5 mi değişmeli) · karar motorunun semantiğini değiştiren bir düzeltme ·
o düzeltmenin **yapısal** bir alternatifi (advisory lock) · **sevk edilmiş bir §5 ihlalinin ne zaman
düzeltileceği** (şimdi mi, devir mi) · bir testin **süre bedeli** (98,5 vs 32,9 sn) · **bir kartın
istediği tablonun gereksiz olduğunun ölçülmesi** (migration + retention altyapısı → 13 satır) ·
**§4.6 eşiğinin nerede duracağı** (kayıtsız 400 bandı) · ve **şemanın sağlamadığı bir önkoşulun nasıl
kurulacağı** (admin girişinde tenant çözümlemesi).
**Kalıp:** ajan **ölçer ve iki okumayı önüne koyar**, kararı vermez; orkestratör soruyu **ölçümle**
sorar (seçenek başına somut sayı), cevap `state.md` oturum günlüğüne **tarihiyle** yazılır ve kart/ADR
ona atıf yapar. **Gözlem:** on sorunun **altısı** ancak bir ajan bir şeyi **ölçtükten sonra** doğdu —
yani "kullanıcıya ne soracağımı" başta bilmiyordum, **ölçüm üretti**. Brief'e *"ölç ve iki okumayı
önüme koy, karar verme"* yazmak bu yüzden bir nezaket değil, **soruyu var eden mekanizma**.

**Bir görevi A/B fazına bölmek — iki kez işe yaradı (M5-02, M6-01).** Ölçüt **kapsam değil, DENETİM
MERCEĞİ**: bir görev hem **veri katmanı** (migration · RLS · resolver · GRANT) hem **uygulama katmanı**
(handler · ekran · oran sınırı · bağımlılık) içeriyorsa, ikisi **farklı saldırılarla** denetlenir ve tek
commit'te ikisi de yüzeysel kalır. Bölünce A fazı `tappa-db-migrator`'a gider ve güvenlik denetçisi
**yalnız çevrelenmiş bypass'a** bakar; B fazı ayrı bir denetim turu alır ve A'nın yazdığı **yükümlülük
listesini** miras alır. M6-01'de A fazının en değerli çıktısı kod değil, **B'ye devredilen beş
yükümlülüktü** — biri çapraz-tenant kimlik atlatmasıydı ve **B yazılmadan önce** bulundu.

**Sabitler:**
- **Denetçiler PAYLAŞILAN Postgres'e karşı SIRALI koşar** (M3-02 dersi) — DDL/mutasyon sondaları
  birbirini bozar. Salt-okuma sondaları eşzamanlı güvenli.
- **Denetçi raporu kullanıcıya olduğu gibi aktarılır.** Ölçüm sayıları (satır sayısı, `last_ctr`,
  429 sayısı) **özetlenmez** — kanıtın kendisi onlar.
- **Yapıcı ve denetçi `state.md`/`roadmap.md`/`backlog.md`/`open-questions.md`'ye DOKUNMAZ ve commit
  ATMAZ** — ikisi de orkestratörün. Görev **kartını** düzeltmek serbest (tarihli blok).
- **Alt ajanın kendi hatasını raporlaması iyi işaret**, cezalandırma. M5-04'te yapıcı mutasyonunun
  yeşil kaldığını kendi söyledi; M5-05'te dört aşırı iddiasını kendi indirdi; M5-06'da bozuk bir
  baseline'a karşı koştuğu **tüm bir mutasyon partisini** kendi iptal edip yeniden koştu.
- **🔴 DENETÇİ MUTASYONUNU `git checkout`/`restore`/`stash` İLE GERİ ALMAZ.** İş commit edilmemiş
  olduğu için bu, yapıcının o dosyadaki **tüm** çalışmasını siler — M5-06'da bir denetçi böyle **12
  satırı kalıcı** sildi (yeniden yazılmak zorunda kaldı). Doğrusu: değiştireceğin dosyayı **tam yolu
  koruyarak** scratchpad'e kopyala, mutasyonu uygula, **kopyadan geri yaz**. `basename` ile kopyalama
  da yasak — `internal/handler/checkin.go` ve `internal/domain/checkin/checkin.go` birbirini ezer
  (M5-06'da oldu; `git hash-object` ile birebir kurtarıldı). Denetçi brief'i **`git diff | shasum`
  değerini başta ve sonda** istemeli. Uzun komutlarda timeout'u yüksek tut: M5-06'da 2 dk'lık timeout
  bir mutasyonu ağaçta bıraktı.
- **Yapıcı yanlış bir düzeltme talimatına ÖLÇÜMLE itiraz edebilir ve etmeli.** M5-06'da bir denetçi
  *"büyük harfli `<META>` `.templ`'den üretilemiyor"* dedi; yapıcı `make templ` çıktısıyla çürüttü
  (templ büyük harfi **birebir koruyor**) ve doğru bir cümleyi zayıflatmak yerine kanıtla güçlendirdi.
  Orkestratör bunu **teşvik etmeli** — brief'teki bir iddia da ölçülmemiş olabilir.
- **Ürün kararı gerektiren yerde kullanıcıya sor** (§9 tap ekranı, GDPR saklama süresi, WiFi alanı,
  marka token'ı/kontrast) — varsayma. Cevap `state.md` oturum günlüğüne **tarihiyle** yazılır.
- **🔴 `state.md` DENETİMİN KAPSAMINDADIR — ve orada bulunan hata ORKESTRATÖRÜNDÜR.** Damga kontrastı
  işinde denetçinin **tek bloklayan bulgusu** koda değil `state.md`'ye çıktı: değişiklik, orkestratörün
  dakikalar önce yazdığı devir notlarını yanlışlamıştı (*"kullanıcı kararı bekliyor"*, *"opacity:.8"*,
  *"1.52:1"* — üçü de artık yanlış). Alt ajanlar o dosyaya **dokunamadığı** için bunu yalnız denetçi
  yakalayabilir. **Bir görevi kapatan değişiklik, o görevin devir notlarını da kapatmak zorundadır**;
  denetçi brief'i `state.md`'nin ilgili bölümünü **açıkça** denetim listesine koymalı. Aksi hâlde sonraki
  oturum var olmayan bir kusuru miras alır — ki `state.md`'nin varlık sebebi tam olarak bunun tersidir.

## Şimdiye kadar öğrenilenler

Her biri gerçek bir turda çıktı; tekrarlanmasın diye burada:

| Ders | Nereden çıktı |
|---|---|
| `pkill -f 'exe/tappa'` hiçbir şey eşleştirmez; `go run`'ın çocuğu build cache'te çalışır. Temizliği **portla** doğrula. | M0-01, 1. tur |
| `.env`'i Makefile yüklüyor (`-include .env` + `export`); çıplak `go run ./cmd/tappa` **her zaman** config hatası verir — beklenen. | M0-01, kart hatası |
| `go mod tidy` import edilmeyen modülleri düşürür; M1'de ilk import gelene kadar çalıştırılmaz. | M0-02, kart hatası |
| Kriter düzeltirken **düşürdüğün garantiyi** de gerekçelendir; yoksa sessizce zayıflatırsın. | M0-02, 2. tur |
| Dışlayıcı listelerde **sabit olan sayı değil, listedir** (pgx zinciri M1'de büyür). | M0-02, 3. tur |
| `sqlc` override'ında paket adını **sqlc ekler** → `type: "UUID"`, `"uuid.UUID"` değil. `go_type` tam yol ister → `"net/netip.Addr"`. | M0-04 |
| `emit_pointers_for_null_types` **override'ı olan tipi kapsamaz**; her override'ın nullable ikizi elle yazılır. | M0-04 |
| `make audit` `govulncheck`'te durursa `redline-check.sh` **hiç koşmaz** — "audit kırmızı" ≠ "redline denetlendi". | M0-02 |
| Go toolchain Rosetta altında (`darwin/amd64`, makine arm64); yükseltirken mimari bilinçli seçilmeli. | M0-04 |
| **Ölçüm doğru, çıkarım yanlış** olabilir. Yapıcı sondaj sayılarını doğru okudu, "sonuç" cümlesini iki kez ters kurdu. Denetçi ölçümü tekrar etmekle kalmayıp **iddianın kendisini** ölçmeli. | M0-03, 1. ve 2. tur |
| Bir bulgu bir kartı yanlışlıyorsa **yanlışlanan kartı da düzelt.** Bulguyu yalnız kendi kartına yazmak repoda iki çelişik cümle bırakır ve okuyanın öbür kartı açmak için sebebi yoktur. | M0-03, 1. tur |
| **Kriter yazarken "hangi kaçış yolu bunu yener?" diye sor.** "Rolü bağla" yeterli görünüyordu; sorgu şekli serbest kaldığı için kritere tam uyumlu ve hiçbir şey kanıtlamayan bir test yazmak mümkündü. | M0-03, 2. tur |
| **grep tek başına kriter değil.** `tenant_id =` taraması `'x'::uuid = tenant_id`, `tenant_id IN (…)`, `tenant_id::text = …` biçimlerini kaçırır. Mekanik kontrol işaret, bağlayıcı olan düzyazı şart. | M0-03, 3. tur denetimi |
| **Her negatif teste pozitif kontrol eşlik etmeli.** "0 satır döndü" boş tabloda da doğrudur; korumayı kapatınca kırmızıya dönmeyen test hiçbir şey kanıtlamaz. | M0-03, 3. tur denetimi |
| Denetçi **kaçış yolu aramalı** ve denediklerini yazmalı — işlemeyen denemeler de kanıttır. | M0-03, 3. tur denetimi |
| Onay, kusursuzluk demek değil. Bloklayan bulgu yoksa **onay ver**; kalan iyileştirmeler ilgili göreve devredilir. | M0-03, 3. tur |
| **Doğrulama sondası, aracın asgari girdisiyle değil ÜRÜNÜN GERÇEK girdisiyle yapılır.** M0-04'te sqlc `exit 0` verip derlenmeyen kod üretti; M0-07'de redline'ın kapsam kuralı, aranan `tenant_id` dizesi politikanın *zorunlu* yazdığı `app.tenant_id` GUC adının içinde geçtiği için hiçbir gerçek migration'da tetiklenemiyordu. İki vakada da yapıcının sondası "temiz" dedi çünkü gerçekte yazılacak biçimi hiç denemedi. | M0-04, M0-07 |
| **Bir tarayıcı/doğrulayıcı yazıyorsan, onu YENDİĞİNİ kanıtlamadan "çalışıyor" deme.** "Temiz geçti" tarayıcının çalıştığını değil, girdinin o tarayıcıyı tetiklemediğini kanıtlar. Her pozitif teste, korumayı kapatınca kırmızıya dönen bir negatif kontrol eşlik etmeli. | M0-07, 1. tur |
| **Metin işleyen shell aracında string literal / blok yorum / çok-bölümlü dosya (goose Up∕Down) ayrı ele alınmalı.** `sed 's/--.*//'` + `tr ';' '\n'` üçünü de karıştırır; doğrusu durum makineli bir lexer. Maskeleme metni **silmemeli** (yalnız ayırıcıyı değiştirmeli) ki hiçbir `CREATE TABLE` görünmez olmasın. | M0-07 |
| **İstisna/muafiyet mekanizması eklersen: kapsamını daralt (birebir ad, şema kısıtı, dayandığı varsayımı doğrula) ve HER KOŞUDA görünür yap (WARN).** Sessiz muafiyet bir kez yazılır, sonsuza dek denetimi susturur. | M0-07 |
| **Bir tabloda DELETE/UPDATE'i engellemek için GRANT'tan çıkarmak YETMEZ.** `ALTER DEFAULT PRIVILEGES FOR ROLE tappa_owner ... GRANT ..., DELETE ... TO tappa_app` (db-init) her yeni tabloya bu yetkiyi otomatik verir; GRANT yalnız *ekler*. Açık **`REVOKE DELETE ON <tablo> FROM tappa_app;`** gerekir. Ampirik doğrula (`has_table_privilege`) — "GRANT'ta yok" ≠ "yetki yok". M1-06 transactions immutability'sinin (`REVOKE UPDATE, DELETE`) temeli budur. | M1-04 |
| **Bir tasarım kabul edilmiş bir kararı "iyileştiriyor" gibi görünüyorsa, ADR'yi değiştirmeden ADVERSARIAL doğrula.** GUC-anahtar saf-RLS çözümlemesi ADR'nin "saf RLS ifade edemez" iddiasını çürüttü ve daha basit görünüyordu; ama bağımsız tasarım denetçisi iki tek-nokta hatasıyla çapraz-tenant ihlali üretti (SET LOCAL'siz GUC havuzda kalır; `FOR ALL USING` WITH CHECK'i kopyalar). Ders: güvenlik **yapısal** çevreleme ister, disipline bağlı değişmez değil (§4.5 kuşak+kemer). Kararın gerekçesi yanlış olabilir ama kararı doğru — ikisini ayır. | M1-04 |
| **SECURITY DEFINER fonksiyonun kontrol listesi:** owner **asla superuser değil** (yoksa gövde tüm RLS'i atlar = genel bypass); `SET search_path=pg_catalog, pg_temp` (injection); gövdede tablolar **şema-nitelenmiş** (`public.x`); `REVOKE ALL ... FROM PUBLIC` (fonksiyonlar varsayılan PUBLIC EXECUTE alır) + yalnız çağırana GRANT; ihtiyaçtan fazla sütun döndürme; definer rolü en-az-ayrıcalıklı (default privilege yok, yalnız gereken tabloya kolon-SELECT). | M1-04 |
| **sqlc v1.28 `SELECT ... FROM <RETURNS TABLE fonksiyonu>()`'ı tipleyemez** (`column ... does not exist` ya da `interface{}`). Bir fonksiyon-çağrısı sorgusuna ihtiyaç varsa (ör. çözümleme yolu) sqlc'de `-- name:` yazma; sorguyu `internal/db/`'de **elle, tipli** yaz (kaynak SQL'i `-- name:`'siz kanonik belge olarak bırak). Elle-yazılan SQL fonksiyon imzasıyla **elle senkron** kalır → derleyici zorlamaz, sütun sırası testle korunmalı. | M1-08 |
| **`ALTER DEFAULT PRIVILEGES` ile verilen `SELECT INSERT UPDATE DELETE` yüzünden `cidr[]`/dizi/özel tip override'ı `sqlc.yaml`'a eklemeden önce ÖLÇ:** sqlc v1.28 + pgx/v5 `cidr`/`cidr[]`'i zaten `netip.Prefix`/`[]netip.Prefix`'e, nullable `inet`'i `*netip.Addr`'a eşliyor. Gereksiz override eklemek `emit_pointers_for_null_types` ikizini de zorunlu kılar (M0-04 dersi). `make gen`+`go build` ile gerçekten gerekli mi ölç, gerekmiyorsa ekleme. | M1-08 (Q25c) |
| **`go.mod`/`go.sum` değiştiren her görev (yeni/bump'lanan/transitif dep) sonunda `make audit` (govulncheck) KOŞMALI.** `go build`/`vet`/`staticcheck`/`redline` CVE görmez. M1-07 `go mod tidy` pgxpool→`x/text@v0.29.0` transitif getirdi (`GO-2026-5970`); M1-07 denetimi kaçırdı, M1-09'da `make audit` yakaladı. Fix: `go get <mod>@<fixed>`. | M1-07 → M1-09 |
| **redline-check.sh üretim-kodu kuralları (R3 transactions UPDATE/DELETE, R5 DATABASE_MIGRATE_URL) `_test.go`'yu HARİÇ tutar** (`--glob '!**/*_test.go'`, M1-09'da eklendi). Testler bu ifadeleri *reddedildiklerini kanıtlamak için* meşru çalıştırır → düz literal yaz, string-concat ile scanner atlatma (smell). NOT: migration-beşlisi ve SET-LOCAL kontrolleri test dosyalarını da tarar (muaf değil). | M1-09 |
| **Kripto byte-sırası: İÇ-TUTARLI (self-consistent) golden vektör byte-sırası/endian hatasını YAKALAYAMAZ** — üretici ve doğrulayıcı aynı zinciri kullanır, birlikte terslenir. Korrektlik yalnız **DIŞ bilinen-cevap vektörüyle** (gerçek çip / NXP AN12196 / bağımsız kütüphane) kanıtlanır. Ayrıca URL↔SV2 gibi bir aktarımda baytlar **verbatim** geçmeli — "parse (BE) sonra farklı serialize (LE)" reversal üretir. M2-04'te SV2 sayaç byte'ları terslenmişti (BE-parse + LE-serialize); yapısal fix = ham byte'ları verbatim taşı. | M2-04 |
| **§4.7-odaklı güvenlik denetçisi bir DOĞRULUK hatasını kaçırabilir** (mercek red-line'lar: sabit-zaman, sır, ctr). M2-04'te güvenlik denetçisi "temiz, fail-closed" dedi; **genel üçüncü göz** byte-reversal doğruluk hatasını buldu. Kripto/algoritma görevlerinde ikisi de gerekir — biri diğerinin yerine geçmez. | M2-04 |
| **İki denetçi PAYLAŞILAN canlı Postgres'e karşı çalışıyorsa SIRALI koş (veya write sondalarını rollback tx'inde yaptır).** DB-mutasyon denetimi (RLS DISABLE, `ALTER TABLE ... DISABLE TRIGGER`, `migrate down/up`) eşzamanlı iki denetçide birbirini bozar — biri RLS'i kapatırken öbürü izolasyon ölçerse yanlış sonuç. M3-02'de üçüncü göz önce koştu + DB'yi migration-N temiz bıraktı, sonra security-auditor (o da write-sondalarını `BEGIN…ROLLBACK` içinde yaptı). Salt-okuma sondaları (has_table_privilege, pg_policies) eşzamanlı güvenli; mutasyon değil. | M3-02 |
| **🔁 İKİ SAĞLAM KANITIN ARASINDAN GEÇEN MUTASYON.** M5-05'te üretim yazma yolundaki **gerçek bir TOCTOU** tüm suite'i **yeşil bıraktı**: `internal/sun` `AdvanceCounter`'ın N yarışçıyı tek kazanana indirdiğini kanıtlıyor, `checkin` tap'in doğru kayıtla bittiğini kanıtlıyor — ama **tap yolunun onu ÇAĞIRDIĞINI** pinleyen hiçbir şey yoktu. Mutasyon ikisinin **arasından** geçti; yeşil kalmasının tek sebebi `SELECT→UPDATE` penceresinin milisaniye-altı olmasıydı (80 ms'ye genişletilince **12/12 tap SUN-valid**). **Doğru çözüm yarışı daha çok test etmek DEĞİL, ÇAĞRIYI PİNLEMEKTİR:** tüketici tarafında sayan bir arayüz enjekte et ve "tam 1 kez, şu argümanlarla, şu tenant bağlamında çağrıldı"yı iddia et. Kural: **bir garanti A paketinde kanıtlanıp B paketinde kullanılıyorsa, B'nin onu KULLANDIĞI ayrıca pinlenmeli** — yoksa iki yeşil test arasında delik kalır. Ayrıca **dejenere değer** aynı işi bozar: M5-05'te harness debounce'u `DefaultParams()` ile aynıydı, wiring silinince test yeşil kaldı (harness **varsayılan-olmayan** değer sürmeli). | M5-05 |
| **⚠️ ÇIPLAK `go test ./...` HER DB TESTİNİ SESSİZCE SKIP EDER** (`DATABASE_URL` yok). Makefile `-include .env` + `export` yaptığı için yalnız **`make test`** gerçek Postgres'e karşı koşar. Yani "N test geçti, 0 SKIP" iddiası **hangi komutla** ölçüldüğünü söylemeden anlamsızdır — ve çıplak `go test` §4.4/§4.5/§4.6 hakkında **hiçbir şey kanıtlamadan** yeşil verir. Denetçi: ölçümlerini `.env` yüklü ortamda yap ve `-v` ile **SKIP sayısını** gör. | M5-05 denetimi |
| **🔁 DENETLEDİĞİ ŞEYİ KENDİSİ KURAN TEST, HİÇBİR ŞEY DENETLEMEZ.** M5-04'te `NewTap` `TapLimiter`'ı `Audit` olmadan kurdu → M5-03'ün **onaylanmış** kriteri ("429 + tenant çözülmüşse `audit_log` satırı") üretimde **ölüydü** (15×429, çözülmüş `employee_id`, `audit_log` değişmedi). **Hata kimsenin dosyasında değil, iki görevin ARASINDAydı**: M5-03 yeteneği teslim etti, montajı devretti; yanlışlanan cümleler M5-03'ün dosyasında ve kartında, doğru göründükleri hâlde kaldı. **Ve düzeltmenin ilk mutasyonu YEŞİL kaldı**, çünkü red testleri `tp.limiter`'ı kendileri kurup `Audit`'i açıkça geçiriyordu. Aynı tuzak **bir alan yanında** tekrar bulundu (`Refused: nil` de yeşil kalıyordu). Kural: **mutasyon testi, ürünün GERÇEKTEN kurduğu nesneyi sürmelidir** — üretim bütçesi/parametreleri dâhil; test kendi kurduğunu ölçüyorsa montaj hatası görünmez. Bir görev bir yeteneği "teslim edip montajı devrediyorsa", devralanın denetiminde **o kriterin üretimde canlı olduğu** ayrıca ölçülmeli. | M5-04, 1. ve 2. tur |
| **🔁 BİR DEĞERİ DOĞRULAYAN KONTROL, O DEĞERİ KULLANAN KODUN GÖRDÜĞÜ BİÇİMİ GÖRMELİDİR.** Bu oturumda **üç kez** aynı şekilde kaçıldı: `BaseURL="HTTPS://…"` büyük harfli şema `Secure` gevşemesini satın alıyordu (M5-01) · `Sec-Fetch-Site: Cross-Site` büyük harfle çerez-ekim korumasını atlıyordu (M5-02) · `TAPPA_TRUSTED_PROXIES=::ffff:0.0.0.0/96` varsayılan-rota kapısına `/96` görünüp çözücüde `0.0.0.0/0` oluyordu, üstelik **kapı bir önceki RED'e cevaben eklenmişti** (M5-03). Kural: kontrol ile tüketici **aynı temsili** görmeli. En sağlam çözüm kanonikleştirmeyi iki yere öğretmek **değil**, **ikinci temsili silmektir** (M5-03: config v4-mapped yazımı reddediyor, httpx düşürüyor — tek yazım kaldı). Kanonikleştirme iki yerdeyse hata zaten oradadır. | M5-01, M5-02, M5-03 |
| **Bir korumanın MALİYETİ, koruduğu şeyin kendisine saldırı olabilir.** M5-02'de `audit_log` DB seviyesinde append-only'dir (`tappa_owner` bile silemez, §4.6). İki handler dalı her istekte bir audit satırı yazıp **hiçbir oran-sınırı penceresi artırmıyordu** → 300 istek 290×429 aldı **ama 300 satır yazdı**; ön koşul yalnızca *bir ölü davet linki*. **§4.6'nın koruduğu iz, kendi bağışıklığı yüzünden silah oldu.** Kural: **silinemez bir tabloya yazan her yol sınırlı olmalı**, ve "reddettim" ≠ "maliyetsiz" — 429 dalı da DB'ye yazıyorsa o da bir başarısızlıktır ve sayılmalıdır. Ayrıca ayrımı cümlede sakla: *"meşru akış sınıra değmez"* akışın **kendi katkısı** için doğru olabilir ama akışın **servis edilip edilmediği** için yanlış olabilir (tek per-IP kovası, 60 kimliksiz istekle geçerli aktivasyonları kilitliyordu). | M5-02 B, 2. ve 3. tur |
| **Bir yorum "hiçbir çağıran X yapamaz / yapısal olarak imkânsız" diyorsa, X HARİCİ BİR PAKETTEN denenmiş olmalıdır.** M5-01 bu sınıftan **iki RED** üretti: (1) `Token`'ın unexported `string` alanı — `fmt`, alan unexported olduğunda (`CanInterface()==false`) `Formatter`/`Stringer`/`LogValuer`'ı **atlar** ve içteki değeri basar; test yalnız *exported* alanlı sarmalayıcıyı denediği için yeşildi ve hiçbir şey kanıtlamıyordu. (2) `Cookies{secure bool}` — **Go'da yasak olan alanı ADLANDIRMAKTIR, `T{}` yazmak değil**, dolayısıyla sıfır değer (`var c pkg.T`, `T{}`, yazılmamış struct alanı, embedded, kanal, `reflect.Zero`) güvenliği kapatıyordu. **İki yapısal kalıp:** sırrı **dolaylı** tut (`*string` → yazdırınca adres) ve **bayrağın kutbunu tehlikeli durum sıfır-değer OLMAYACAK şekilde seç** (`insecure bool`, `secure bool` değil) — böylece tehlikeli durum *kazara temsil edilemez*. Ölçemediğini iddia değil **sınır** olarak yaz. | M5-01, 1. ve 2. tur |
| **🔁🔁 BİR KORUMAYI ANLATAN CÜMLE, KORUMANIN KENDİSİNDEN TEHLİKELİDİR — ve düzeltme turları bu kusurun ANA ÜRETİCİSİDİR.** M5-06 **15 tur, 11 RED** sürdü; ilk ikisi ekranın metnindeydi, **kalan dokuzu düzeltmenin içinden** çıktı. Her tur bir koruma ekledi ve her tur o korumanın **kapsamını abartan** bir cümle yazdı; sonraki denetçi cümleyi yendi. Zincir: elle kurulmuş bayt-golden üretimin **hiç render etmediği** bir gövdeyi pinliyordu (Note'suz **971 B** vs gerçek **1061 B** — *sonda ürünün GERÇEK girdisiyle yapılır*) → metin-düğümü listesi CSS `content` / `</main>` dışı / `aria-label` / `title` ile yenildi → `<input readonly value>` (*"value machine-facing'dir"* yanlıştı) → `<iframe srcdoc>`/`<object data>`/`<img src=data:svg>` (**işi bir özniteliği göstermek olan eleman**) → `<link href="data:text/css,…{content:'…'}">` (**izinli eleman + okunmayan öznitelik**) → metin testi bir dalı (`RetryURL != ""`) **hiç render etmiyordu** → `<meta http-equiv=refresh>` → ve o kontrolün regex'i **öznitelik SIRASINA bağlıydı**. **İki kural çıktı:** (1) *kara liste → beyaz liste → **kapalı küme***: yenilemeyen tek şekil, iki yönlü küme eşitliği olan bir **etiket kümesi** oldu (listede olmayan eleman **ve** kimsenin render etmediği ölü girdi, ikisi de testi kırar). (2) **KAPANIŞ KURALI:** bir noktadan sonra *"yeni kanal kapatılmıyor, dürüstçe **LİMİT** olarak sayılıyor"* denmeli — bu gevşeme değil, çünkü sayılmış bir açık, kapatıldığı **iddia edilen** bir açıktan güvenlidir. Ve hiçbir yerde *"tamamen / bitmiş / complete / exhaustive"* yazma: yazmadan önce **yenmeye çalış**, yenemediysen **nasıl denediğini** de yaz. | M5-06, 15 tur |
| **TAILWIND `.templ` DOSYALARINI YORUMLAR DÂHİL HAM METİN OLARAK TARAR** — bir yorumda geçen `opacity-80`, `ring`, `hidden`, `p-4` gibi bir kelime **gerçek bir CSS kuralı derletir**. Ölçüldü: iki isim = **+330 bayt**, dört isim = **+348 bayt**, üç yeni seçici. Ve tuzak **zaten ateşlenmiş**: bugünkü `app.css` düzyazıdan doğmuş **yedi ölü kural** taşıyor — `.filter` (185 B) · `.visible` (28) · `.relative` (28) · `.min-h-16` (26) · `.static` (24) · `.fixed` (22) · `.hidden` (21) = **334 bayt, dosyanın %2.34'ü** — hiçbiri **95 `class` özniteliğinin** hiçbirinde yok; kaynakları *"NO verdict filter"*, *"a visible edit"*, *"min-h-16 = 64px"* gibi cümleler ve `/static/…` URL'leri. (`.relative` ve `.min-h-16` gerçekten **kullanılıyor** ama `@apply` ile, yani inline oluyor — bağımsız kurallar yine de gereksiz.) Zararsız (ölü CSS) ama **iki yönü var**: (a) yorumda sınıf-adı gibi görünen kelimelerden kaçın, (b) *"bu sınıf derlenen CSS'te var"* ölçümü, sınıfın bir **yorumda** geçmesiyle de sağlanabilir — yani sınıfın gerçekten **kullanıldığının** kanıtı değildir. | Damga kontrastı (M5-06 sonrası) |
| **🔬 EŞZAMANLILIK ÖLÇÜMÜ SESSİZCE ARTEFAKT ÜRETİR — üç kez yaşandı, ikisinde ölçen kişi DENETÇİYDİ.** M5-08'de (a) *"advisory kilit masumdur"* kontrol grubu, flood'u **tek oturumdan** sürdüğü için isteklerin çoğunu `BySession` 300/10dk'ya takıp kilide **hiç dokunmadan** 429 yaptırıyordu (200 istek **40 ms**'de bitti) → *"fark yok"*; temiz A/B (flood **ayrı oturumlardan**, kurban **tek atış**, taze oturum) ilgisiz kişinin gecikmesini **6–9×** ölçtü, ve `pg_stat_activity` mekanizmayı **doğrudan** gösterdi (16 bağlantının 15'i `wait_event='advisory'`, kontrol kolunda 0). (b) Bir yarış ölçümü **konfigürasyon başına tek örnek** aldığı için ~%4'lük bir olayı "0 hata" okudu. (c) Aynı sondanın **id-verme şekli üretimden farklıydı** (`policy_attachments`'a paylaşılan PK veriyordu, üretim `id`'yi hiç vermiyor) → yapısal olarak imkânsız bir sonuç üretti. **Kurallar:** yükü **ayrı kimliklerden** sür (oran sınırı kolu daraltmasın) · kurbanı **tek atış** ölç (döngü worst-of-N'i yavaş kola kaydırır) · düşük olasılıklı olay **çok deneme** + her denemede **bakir anahtar** ister · sonda **üretimin verdiği şekli** taklit etmeli · ve mümkünse **gerçek kod yolunu** sür, sentetik yarışı değil. Mekanizmayı **çıkarımla değil ölçümle** göster (`pg_stat_activity`, `EXPLAIN (ANALYZE, BUFFERS)`, log satırı). | M5-08 |
| **SAYI HATALARI TEK BAŞINA BİR BULGU SINIFIDIR.** Bir yorumdaki sayı, sayarak yakalamak için var olan tek teldir; yanlışsa tel gerilmez. M5-06'da altı kez bloklayan oldu: `SEVEN FIELDS`→**8** · "üç aktivasyon ekranı"→**5** (yani ortak şablon 9 değil **11** ekrana düşüyor) · "dört dal"→**5** · "iki vaka"→**4** · "yedisini de pinliyor"→**6** · *"kapanış cümlesi ~16:1"*→**5.70:1** (o değer **başlığın**dı; kapanış cümlesi `</section>`'dan **sonra** render ediliyor, yani `text-ink/70` **porcelain** üstünde). Çare: **sayıyı bir tele bağla** — `TestResultView_FieldCountIsTheSpec` tip üzerinde reflection yapıyor, alan eklenince kırılıyor. Bağlayamıyorsan sayıyı **her turda yeniden türet**, kopyalama. Ve **ölü atıf da bu sınıftır**: yorumdaki her tanımlayıcıyı (`grep -rIoh -F`) repo genelinde ara — M5-06'da dört tane yalnızca kendi yorumunda yaşayan test/değişken adı bulundu. | M5-06 |
| **Migration testleri random-UUID fixture'ı COMMIT ediyorsa dev-DB şişer ve append-only+REVOKE DELETE yüzünden temizlenemez** (imkânsızlık = izolasyon garantisi, M1-09). Bu bir hata değil sonuç; denetçi "DB temiz" derken kendi rollback'li sondasını kasteder, tablo boş demek değil. Gerçekten izole ölçüm gerekiyorsa testcontainers/ayrı DB (M8). | M3-02 |
| **🔬 BİR DENETİM ARACININ KAPSAMI, ONUN İDDİASININ PARÇASIDIR — ve M5-09'da ÜÇ araç aynı anda kendi kapsamından yakalandı.** (1) `make audit`'in `SRC`'si `test/`'i **hiç taramıyordu** → yeni dört dosyanın **ikisi** denetim dışıydı ve *"make audit temiz"* göründüğünden az şey kanıtlıyordu (eklendi, sıfır yanlış pozitif; ayrıca R7 desenleri **tek satırlık** — `fmt.Fprintf(&b,` + duyarlı dize sonraki satırdaysa ağdan geçiyor). (2) `assertAfterShiftStart` **yapısal olarak boştu**: beklenen an tanımı gereği geçmişte, ölçülen an ≈`now` → **hiçbir motor hatasında** kırmızıya dönemezdi (mutasyonla kanıtlandı: geç kalma hesabı **tümden** ölse bile yeşil kaldı). (3) `assertTellableApart` **dejenere değer tuzağına** düştü — beklenen ve fiili değerin **ikisi de** aynı `f.runStamp`'tan türüyordu, yani sabit bir damga onu kusursuz tatmin ediyordu; **üstelik bu tuzağın adı aynı dosyanın 86. satırında yazılıydı.** Kural: bir kontrol yazdıktan sonra **onu tatmin eden en dejenere girdiyi** ara — testin iki tarafı da aynı kaynaktan besleniyorsa test bir totolojidir. Ve *"N dosya tarandı / M kontrol geçti"* demeden önce **kapsamı** ölç. | M5-09 |
| **🔴 BİR FIXTURE'IN "ETRAFINDAN DOLAŞTIĞI" ŞEY BİR ÜRÜN HATASI OLABİLİR — ve o workaround'u YAZAN kişi bunu görmeli.** M5-09'un gün simülasyonu, çalışan bir senaryoyu yeşile almak için tek bir alanı beyan ediyordu (`declaring(...)`); yapıcı bunu *"gizlemedim, etrafından dolaştım"* diye yazdı, ve o cümle bir **§5 ihlalini** ortaya çıkardı: `practice` satırı daha eski bir **açık girişi maskeliyor**, çıkış `in` oluyor, giriş **hiç kapanmıyor**, **hiçbir sinyal yok**, ve **düz HTTP'den erişilebilir**. Denetçi bunu fixture'ın **kendi testiyle** kanıtladı (workaround kaldır → RED). Kural: bir fixture bir davranışı **beyan ederek** düzeltiyorsa, o beyanı kaldırıp ne olduğunu **ölç** — "test geçsin diye" yazılan her satır bir hipotezdir. Ve bir test görevinde bulunan ürün hatası **test görevinde düzeltilmez**: kendi kartını (ve gerekiyorsa ADR'sini) hak eder, ama **kalıcı bir yere yazılmadan** görev kapanamaz. | M5-09 |
| **Bir "iyileştirme" ölçülmeden reddedilmez, ama ölçülünce KENDİ ARIZASINI ürütebilir.** M5-09'da süiti hızlandırmak için `t.Parallel()` denendi: **3 koşu, 3 farklı testte kırmızı**, tek kök — testler **aynı seed'li plaketin `last_ctr`'ını** paylaşıyor ve her tap `last_ctr+1` okuyor, yani paralelde sayacı başkası ilerletince tap **replay** sayılıyor. Biri **§4.4 kırmızı-çizgi testini OLMAYAN bir ihlalle** kızarttı. İki ders: (a) bir hızlandırma önerisi **kararlılıkla birlikte** ölçülmeli (tek yeşil koşu kanıt değil, **art arda üç**); (b) **yanlış alarm üreten bir kurulum başlı başına risktir** — §4.4 gürültü üretmeye başlarsa gerçek ihlal o gürültüde kaybolur. | M5-09 |
| **🔴 BİR ŞEMA, SAĞLANMAYAN BİR ÖNKOŞULU VARSAYABİLİR — ve bunu ancak o şemayı KULLANMAYA çalışınca görürsün.** `admin_users` (M1-11) *"resolver YOK: giriş tenant'ı **biliyor**"* yazıyordu ve **hiçbir şey tenant'ı kurmuyordu**: e-posta yalnız `(tenant_id, email)` içinde tekil, `tenants`'ta slug yok, ürün tek bir `/api/auth/login` gösteriyor. Şema doğruydu, testleri geçiyordu, denetimden geçmişti — **kullanılamıyordu**. Kural: bir tablo indiğinde *"bu satırı **kim**, **hangi bağlamda**, **hangi girdiyle** okuyacak?"* diye sor ve **o yolu uçtan uca yürü**; yürüyemiyorsan eksik olan şema değil **önkoşuldur** ve onu ilk kullanıcı bulur. (Burada bulan görev **beş ay sonraki** M6-01'di ve çözüm bir **kullanıcı kararı** gerektirdi.) | M6-01 A |
| **🔬 SABİT `search_path`, EŞİTLİĞİ SESSİZCE DEĞİŞTİREBİLİR.** SECURITY DEFINER fonksiyonlarında `SET search_path=pg_catalog, pg_temp` **zorunlu** (injection savunması, ADR 0002 md.7). Ama `citext`'in `=` operatörü **`public`** şemasındadır: sabit path altında **görünmez** ve Postgres **hata vermeden** `text=text`'e düşer → bir **kimlik doğrulama araması** sessizce **harfe duyarlı** olur (ölçüldü: `Owner@Example.Test` kayıtlıyken küçük harf **0 satır**, büyük harf **0 satır**). Çare `OPERATOR(public.=)`. **Üç ders:** (a) tuzak **`search_path` özelliğidir**, SECURITY DEFINER'a özgü değil — düz bir sorgu da `SET LOCAL search_path` altında aynı şeyi yapar; (b) **uzantı tipi kullanan her karşılaştırma** bu sınıftadır (`citext`, `hstore`, `postgis`…) ve şemanın hangi sütunlarının o tipte olduğunu **saymak** gerekir; (c) sonuç bir **alt küme** olduğu için fail-closed'dır (giriş yapılamaz), ama bir **üst küme** üreten bir varyantı düşün — o kimlik doğrulama **atlatması** olurdu. | M6-01 A |
| **🔴 İKİ ÇARE BİRBİRİNİN TERSİNE ÇEKEBİLİR — ve gerilim yazılmazsa biri diğerini SİLER.** M6-01 A'da B fazına dört yükümlülük yazıldı; güvenlik denetçisi **beşincisini** buldu ve o, listedeki **en ağırıydı**: *oturum yalnızca hash'i eşleşen adaya verilir* (yoksa saldırgan kurbanın e-postasını **kendi** tenant'ına yazar, **kendi satırında** doğrulanır, ve seçicide **kurbanın işletmesini** seçer — §4.5 çapraz-tenant kimlik atlatması, canlı ölçüldü). **Ağırlaştırıcı:** DoS çaresi olarak üç belgede önerilen *"ilk eşleşmede dur"*, **"ilk eşleşmede dur + seçicide tüm adayları göster"** biçiminde uygulanınca **tam olarak bu atlatmadır**. Kural: bir listeye iki azaltma yazarken **birbirlerini nasıl bozabileceklerini** de yaz — *"her ikisini de uygula"* bir tasarım değil, iki ayrı cümledir; gerilim yazılmazsa uygulayan kişi ucuz olanı seçer. | M6-01 A |
| **🔁🔁🔁 "BİR CÜMLE, SİSTEMİN VERMEDİĞİ BİR ŞEYİ BEYAN EDİYOR" — BU SINIF, KENDİ DÜZELTMESİNİN İÇİNDE YENİDEN DOĞUYOR.** M5-11 tam olarak bu sınıftan bir **ürün hatasını** düzeltti: `decide.go` *"birincil koruma çağıranın sorgusudur, **ki practice'i dışlar**"* diyordu, **sorgu dışlamıyordu**, ve aradaki boşluk sevk edilmiş kodda bir **§5 ihlali** üretiyordu (açık giriş hiç kapanmıyor, müdüre *"unutulmuş çıkış"* gibi görünüyor, **hiçbir sinyal yok**). Düzeltme tek satırdı. **Ama düzeltmeyi anlatan ADR'nin ilk hâli AYNI HATAYI yaptı:** *"düzeltme yolu mekanizma olarak **zaten var** … + `audit_log`"* — güvenlik denetçisi ölçtü: **408 manuel satır / 0 `audit_log` satırı / 0 HTTP rotası**. Yani ADR, kendi 88-100. satırlarında *"bu projenin en pahalı bulgu sınıfı"* diye adlandırdığı şeyi **kendi içinde tekrarladı**. **Kural: bir mekanizmanın VAR olduğunu yazmadan önce onu ÇAĞIR ve SAY.** *"§5 emrediyor"* ≠ *"kod yapıyor"*; *"domain metodu var"* ≠ *"yol var"*. Ve bu sınıfın **en tehlikeli** biçimi budur: doğru bir düzeltmeyi anlatan yanlış bir cümle, sonraki okuru **aramaktan alıkoyar**. | M5-11 |
| **📏 ORKESTRATÖRÜN KARTI DA ÖLÇÜLMELİ — ve yapıcı onu ÇÜRÜTEBİLMELİ.** M5-11 kartını ben (orkestratör) yazdım ve içine *"gerçek hayat senaryosu: kayıp telefon → yeniden aktivasyon → practice satırı maskeliyor"* koydum. **Yanlıştı:** `isPracticeTap` `LastForPerson == nil` istiyor (practice = *"hiç kaydı yok"*, *"her aktivasyondan sonraki ilk"* değil) **ve** `ConsumeInviteAndActivate` `activated_at`'i `COALESCE`'luyor — üstelik ikincisi **repoda zaten yazılıydı**. Yapıcı 0,26 sn'lik bir ölçümle çürüttü ve kartı düzeltti. **Kusurun gerçek erişilebilir yolu bambaşkaydı** (geriye tarihli `occurred_at`, yani **M9-01 kuyruğunun ürettiği şekil**) — yani yanlış senaryo, kusurun **kime ve nasıl** çarptığını da yanlış gösteriyordu. **Kural: brief'e koyduğun senaryo bir HİPOTEZDİR; yapıcıdan onu ölçmesini iste, ve çürütürse kartı düzelttir.** Orkestratörün cümlesi de sistemin vermediği bir şeyi beyan edebilir. | M5-11 |
| **🔍 BİR KARTI UYGULAMADAN ÖNCE KARTI ÖLÇ — özellikle bağımlılığı KARTTAN SONRA inmişse.** M5-10 kartı bir migration + RLS beşlisi + **sıfırdan retention altyapısı** istiyordu; ölçüm kartın **M5-04'ten önce** yazıldığını ve M5-04'ün bu arada kartın istediği `first_seen_at`'i **imzalı bağlamda, MAC'in içinde, sunucu saatiyle** zaten sağladığını gösterdi. Doğru cevap **13 satırdı**. Aynı ölçüm kartın **sekiz cümlesini** yanlışladı (biri kabul kriteriydi ve **hiçbir tasarımla** karşılanamıyordu; biri **zaten inmiş** bir işi M5-10'a fatura ediyordu). **Kalıp:** karta başlamadan önce *"bu kart hangi tarihte yazıldı, o günden beri hangi bağımlılık indi, kart neyi hâlâ doğru varsayıyor?"* diye sor ve **ölç**; kart ile gerçek çelişiyorsa **kartı düzelt** (kriterleri silmeden, tarihli blokla). Bu ölçüm bir migration'ı ve bir haftalık işi sıfıra indirdi. | M5-10 |
| **🔴🔴 BİR KURAL, YAZILDIĞI HATAYI ELEMİYORSA KURAL DEĞİLDİR — ve SABİT LİSTELİ TEST BİR AĞ DEĞİL, DEĞİŞİKLİK DEDEKTÖRÜDÜR.** M5-10'da guardrail sırası düzeltildikten sonra dosyadaki genel yerleştirme kuralı şöyleydi: *"§5'in adlandırmadığı bir guardrail, sırayı bozmayan **her yere** konabilir."* Denetçi ölçtü: **regresyonu üreten sıra da bu kuralı tam olarak sağlıyordu** (§5 satırlarının göreli sırası her iki dizilimde de korunuyordu). Kural, yazılma sebebini elemiyordu. Yerine **yapıyı** kontrol eden invaryant kondu: *uyarı taşıyan bir guardrail'in **önündeki her şey** adlandırılmış istisna olmalı.* İkinci yarısı daha da önemli: sıranın tek mekanik ağı **sabit listeli** bir testti ve o test **yeniden sıralamayı** yakalıyor (her permütasyon listeyi bozar) ama **EKLEMEYE karşı çaresiz** — çünkü kırmızıya dönen testin **doğal onarımı listeyi güncellemektir**, ve bu tam olarak yanlış hamledir. Denetçi bunu sahte bir 11. guardrail'le kanıtladı: **üç paket de yeşil** kalıyordu. **Kural: bir invaryant LİSTEYİ değil ÖZELLİĞİ kontrol etmeli**, yoksa büyümeye karşı sessizdir. Ve her invaryanta **negatif kontrol** koy — kendi kontrolümle doğrulandı: invaryantı içi boşaltılınca iki negatif kontrol de kırmızıya döndü. | M5-10 |
| **🔴 UYUYAN BİR KURALI UYANDIRMAK BİR CONFIG AYARI DEĞİL, DAVRANIŞ DEĞİŞİKLİĞİDİR — ve SIRA DEĞİŞİKLİĞİ GENİŞTİR.** `sys:tap-freshness` guardrail'i aylardır kodda duruyordu ama eşiği bağlamın TTL'iyle **aynı sayıydı**, yani **hiç ateşlenemiyordu** — dolayısıyla listedeki **konumu hiçbir şey ifade etmiyordu**. M5-10 eşiği düşürünce konum bir **güvenlik kararına** dönüştü ve ortaya §5 satır 4 ihlali çıktı: deaktif oturum **3 dakika bekleyerek** güvenlik uyarısını düşürebiliyordu (ve aile taraması aynı kusurun **daha eski, daha ucuz** bir ikizini buldu: `sys:occurred-at-bound`, tetiği bir **form alanı**). **İki kural:** (1) *"bu kural bugüne kadar hiç ateşlenmedi"* cümlesi bir güvence değil, **denetlenmemiş bir alan** demektir — uyandırırken **etrafındaki her şeyi** yeniden değerlendir; (2) bir sıra değişikliği **geniş** bir davranış değişikliğidir: **çapraz çarpımı ölç** (M5-10'da 864 kombinasyon → **78 kazanan değişti**; uyarı kaybeden **0**, kayıt kaybeden 52'nin hepsi üretimde **erişilemez** ve §5'e **uygun** — ama bu ancak ölçülünce bilindi). | M5-10 |
| **🔁 GÜVENLİK MERCEĞİ İLE DOĞRULUK MERCEĞİ BİRBİRİNİN YERİNE GEÇMEZ — bu oturumda ÜÇÜNCÜ kez kanıtlandı.** M5-10'da genel üçüncü göz **üç tur** denetledi, her turda bloklayan buldu ve **hepsi cümle hatasıydı**; diff'in içinde duran **§5 ihlalini** üçünde de görmedi. `tappa-security-auditor` ilk turunda buldu, ölçtü, ve düzeltildikten sonra **kendi RED'ini kendi sondasıyla kapattı**. Tersi de olmuştu (M2-04'te güvenlik denetçisi "temiz" derken genel göz byte-reversal buldu). **Kırmızı çizgiye değen her işte İKİSİ DE koşar**, ve RED'i açan denetçi onu **kapatan** da olmalı. | M5-10 |
| **⏱️ BİR TESTİN SÜRESİ BİR TASARIM KISITINDAN GELİYORSA, ONU "OPTİMİZE ETME" — SAY ve KULLANICIYA SOR.** ADR 0006 debounce'u **sunucu saatiyle** ölçüyor, dolayısıyla aynı kişinin ardışık tap'leri **gerçek saniye** ister ve bir günü sıkıştırmak **imkânsızdır** (beklemesiz koşuda 15 kaydın 10'u `sys:person-debounce`). Buradaki tek yanlış cevap **motoru fixture'a uydurmaktı**. Doğru cevap: bekle, bedeli **ölç** (98,5 sn vs 32,9 sn vs elenen `t.Parallel()`), ve **seçeneği sayılarla kullanıcıya sun**. ⚠️ *"Varsayılana `-short` koyalım"* bedava değildir: bu repoda **"atlanan bir test geçmiş sayılmaz"** yazılı bir derstir (M5-06 md.4). Skip eklenecekse **varsayılan yol dışında** olmalı ve skip mesajı **neyin koşmadığını adıyla** söylemeli. | M5-09 |
| **🔴🔴🔴 BİR DÜZELTMENİN KENDİ AĞI AYNI TURDA ÖLÇÜLMEZSE, DÜZELTME YAZILMAMIŞ SAYILIR — M6-01 B'de BEŞ kez, beşi de ayrı turda, ayrı denetçi tarafından.** Sevk edilen beş korumanın **beşinin de** silinmesi tam suite'i `-race` altında **yeşil** bıraktı: `isLookupableEmail` (adını taşıyan tek test **sahte DB** kullanıyordu ve sahte DB **her bayt dizisini** kabul ettiği için guard olmadan da aynı sonucu üretiyordu) · `sessionGate` (yalnız sabitin **değeri** pinliydi, davranış hiç ölçülmüyordu) · `sameOriginGate` (handler Origin'i **tekrar** kontrol ettiği için **sonuç** değişmiyordu — değişen tek şey **maliyet**, ve middleware'in tek varlık sebebi oydu) · `meterOnly`'nin ücretlendirmesi · ve `CookiePath`/`maxCandidates`'in **totolojik** testleri (beklentiyi sabitin **kendisiyle** yazmak: `if ck.Path != CookiePath` her değer için doğrudur). **Kural: bir koruma eklerken onu ÖLDÜREN mutasyonu AYNI TURDA koş ve çıktısını raporla.** *"Test var"* kanıt değildir; kanıt **testin kırmızıya döndüğü**dür. Ve **sahte bir bağımlılık, gerçeğin reddettiği bir şeyi asla kanıtlayamaz** — DB'nin söz hakkı olan her guard **gerçek DB'ye karşı** pinlenmeli. | M6-01 B |
| **🔴 BİR DESENİ KOPYALARKEN KAÇ PARÇASI OLDUĞUNU SAY — orkestratörün kendi hatası.** `GET /admin` bütçesiz bir `SECURITY DEFINER` resolver okuması ödetiyordu; orkestratör *"reponun kendi sırasını uygula"* diyerek flood kapısını ekletti. Ama `tap.go`'nun deseni **ByAddress → Identify → BySession** üçlüsüdür ve yalnız **ilk aşama** uygulandı. Sonuç bir sonraki turda ölçüldü: kurbanın adres anahtarını paylaşan biri (aynı NAT) 200 isteği yakınca `POST /admin/logout` **429** aldı, `Revoke` **0** kez koştu, ve **sunucu tarafı oturum süresi olmadığı için** o pencerede oturumu bitirecek **hiçbir yol** kalmadı. Eksik olan üçüncü aşama tam olarak *kimliği çözülmüş çağıranın anonim kovayla aç bırakılmasını* engelleyen şeydi. **Kural: "reponun deseni" demek yetmez — deseni SAY, ve her parçasının neyi engellediğini yaz.** İkinci yarı: bir bütçe eklerken *"bu bütçe hangi meşru işlemi reddedebilir?"* diye sor; cevap **"çıkış"** ise değişmez kısıt yazılmalıdır. | M6-01 B |
| **📏 SAYI HİJYENİ BAŞLI BAŞINA BİR BULGU SINIFI — M6-01 B'de ALTI vaka, biri bir korumanın 15× gevşetilmesinin TEK gerekçesiydi.** Vakalar: 00011'in en büyük sayısı (`cost-10 ≈ 60–100 ms`) **~4× iyimserdi** çünkü sevk edilen digest'ler **cost 12** (367 ms) · flood tavanının maliyeti **yanlış kolda** ölçülmüştü (uydurma token 1,2 ms yerine canlı oturum **5,7 ms**, çünkü o kol bir de **UPDATE** yazıyor) → gerçek 4,5 sn değil **17,2 sn** · `make test-short` bandı **üç kez** dar yazıldı ve **üç kez** tutmadı · *"ALL SIX PANEL ROUTES"* diyen yorumun tablosunda **beş** satır vardı ve **aynı yorum** üç satır sonra altıncının dışarıda olduğunu söylüyordu · bir yorum *"audit izi başarılı karşılaştırmayı zaten taşıyor"* diyordu ve o dal **hiçbir satır yazmıyordu** · ve bir düzeltme turu **kendi düzelttiği sayıyı** başka bir dosyada bayat bıraktı. **İki kural:** (1) bir sayı **hangi kolda** ölçüldüğünü söylemeli — *"resolver okuması"* ile *"resolver okuması + UPDATE"* aynı şey değil; (2) tekrar tutmayan bir bant **dar yazılmamalı**, **gözlenen aralık** olarak yazılmalı ve *"gözlem kaydı, hedef değil"* uyarısı taşımalı. Dördüncü kez tutmaması ancak iddia **hedef olmaktan çıkınca** imkânsızlaşır. | M6-01 B |
| **🧨 BİR KÜTÜPHANENİN "BİLİNEN SINIRI" BİR VARSAYIM DEĞİL, ÖLÇÜLECEK BİR DAVRANIŞTIR — ve onu kapatan düzeltme yeni bir kanal açabilir.** Q03 bcrypt'in 72-baytlık sınırını bir not olarak taşıyordu; ölçüm x/crypto'nun **karşılaştırıcısında canlı bir kusur** gösterdi: `GenerateFromPassword(100 bayt)` **hata** verir ama `CompareHashAndPassword(hash(72×'a'), 100×'a')` **nil** döner — yani 72-baytlık öneki paylaşan iki parola **birbirini doğrular**. Sınırda reddetmek doğru karardı. **Ama düzeltme bcrypt'i KISA DEVRE yaptırdı** ve kukla yalnız *"hiç aday yok"* dalında ödendiği için **kayıtlı e-posta 5,53 ms, kayıtsız 295,42 ms = 53×** oldu: tek istekle, istatistiksiz, kimliksiz bir e-posta numaralandırma kehaneti — ve üç yerde *"kapalı"* diye yazılıydı. **Kural: bir erken dönüş eklerken "bu dalı kim seçiyor — saldırganın girdisi mi, SUNUCU DURUMU mu?" diye sor.** Cevap sunucu durumuysa, dal **aynı maliyeti ödemelidir**. Ve o dalı test tablosuna **ayrı bir kol** olarak ekle: üç kolun üçü de menzil içi parola kullanıyordu, bu yüzden hiçbiri görmedi. | M6-01 B |
| **🔢 BİR SAYIYI YAZARKEN DEĞERİNİ DEĞİL, ETİKETİNİN HANGİ BÜYÜKLÜĞÜ GÖSTERDİĞİNİ KONTROL ET — M6-02'de BEŞ turda BEŞ kez, beşi de aynı kökten.** Vakalar: (a) düzyazı *"PORCELAIN IS THE BINDING GROUND"* derken **aynı turun kendi tablosu ve kendi test dosyası** `green-lite` diyordu — iki cümle **aynı commit'lenmemiş değişikliğin** içindeydi, ve kod doğru olduğu için hiçbir test itiraz etmiyordu; (b) tek bir cümlede **yük bir paydayla (260 = görüntüleme + giriş), pay başka bir paydayla (200 = yalnız görüntüleme)** hesaplanmıştı → yazılan **15×**, gerçek **11,5×**, ve hata **%30 iyi** okunuyordu; (c) *"türetilen çağrı yeri sayısı"* diye basılan sayı **dosya sayısıydı** (5), gerçek **12** — üstelik **aynı satırdaki döküm 12'ye toplanıyordu** ve bu, bir önceki turun *"artık sayı basılıyor"* diye sunduğu **kanıtın kendisiydi**; (d) bir anti-vacuity kapısı **farklı ton sayısını** sayıp mesajında *"there are dozens"* diyordu (o "dozens" **tekrarları** tarif ediyordu) → **meşru** bir sadeleştirme (`/85`→`/70`) taramanın *"yanlış dosyaları okuyorsun"* diye kırmızı vermesine yol açıyordu. **İki kural:** (1) bir sayı **hangi kolda/paydada** ölçüldüğünü **yazmalı** — *"resolver okuması"* ile *"resolver okuması + UPDATE"* aynı şey değil, *"dosya"* ile *"çağrı yeri"* aynı şey değil; (2) bir sayıyı **raporlayan** kodun, o sayının **adını taşıdığı şeyi izlediğini** mutasyonla kanıtla (M6-02'de ölçüldü: rapor 5→4 düşüyordu ama **beş çağrı yerli bir dosyadan dördü silinince hiç değişmiyordu**). ⚠️ Ve bu sınıfı düzeltirken **düzeltmenin kendi ağını aynı turda ölç** — yapıcı ilk denemesinde **çift saydı** (24), kendi mutasyonuyla yakaladı. | M6-02 |
| **🎨 SEVK EDİLMİŞ BİR KONTRAST HATASI ANCAK TÜRETİLMİŞ BİR TEST YAKALAR — ve "bağlayıcı zemin" ayrı bir iddiadır.** M6-02 `.docket-label`'ı **3,13:1** ile sevk edilmiş buldu (AA 4,5:1), **12 çağrı yerinde**, **çalışanın kutsal tap ekranı dahil**; artı beş ton daha 2,40–4,36:1 arasındaydı. Hiçbiri yıllardır fark edilmemişti çünkü **üründe otomatik kontrast ağı yoktu — sayılar yalnız düzyazıdaydı.** ⚠️ **İki ayrı şey pinlenmeli:** ton **değerleri** *ve* **hangi zeminin bağlayıcı olduğu**. İkincisi unutuldu ve tam da beklenen şey oldu: iki tur (**ve orkestratörün brief'i**) `porcelain`'i bağlayıcı sandı çünkü o **sayfa zeminidir** — oysa karar **bağıl parlaklıkla** verilir ve `green-lite` (0,8229) porcelain'den (0,8627) **koyudur**. Yakalayan şey **türetilmiş test** oldu; elle yazılmış bir sayı olsaydı bugün de yanlış olurdu. **Kalıp:** ağ (i) paleti **config'den yeniden türetsin** (hex'in ikinci kopyası olmasın), (ii) **sıfır çağrı yerinde koşmayı REDDETSİN** (bir ton her zeminde kusursuz olup ürünün artık render etmediği bir sınıfı koruyabilir), (iii) düzyazının adlandırdığı zemini **aritmetiğin türettiğiyle** karşılaştırsın — ve **işaretin SİLİNMESİ de kırmızı vermeli**, çünkü düzyazı okuyan bir testi geçirmenin en doğal yolu cümleyi silmektir. **Ve `app.css`'e bağımlı olmasın:** üç `TestCompiledCSS_*` testi CI'da **daima SKIP** eder (`app.css` gitignore'da, CI `make css` koşmuyor) — *bir skip pass değildir.* | M6-02 |
