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
