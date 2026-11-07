# ADR 0039 — Süreç geneli bcrypt kapısı: panel parola işi yük atar, tap atmaz

- **Durum:** **Kabul edildi (2026-11-07).** Backlog T86 kapandı; güvenlik denetimi onay (bir ORTA bulgu bu ADR'nin §3'ü), üçüncü göz düzeltmeleri bu turda.
- **Tarih:** 2026-11-07 · **Backlog:** T86, T90 · **Dal:** `m14-budgets`
- **İlgili:** ADR 0031 §5.6 (sabit zamanlı giriş) · ADR 0020 (operatörün kendi bütçeleri) · ADR 0024 §2.6 (logo çözme semaforu, aynı biçim) · CLAUDE.md §4.6
- **Ölçüldü** = bu dalda, yerel iş istasyonu (16 çekirdek, go1.26.10, paylaşımlı makine, yük belirtildi). **Okundu** = dosyadan, çalıştırılmadı. **(denetimde)** = sayı denetçiden geldi, hesap, ölçülmedi.

## 1. Sorun

Panel parola işinin (cost-12 bcrypt) önündeki her bütçe adres, hesap ya da link başınaydı (`internal/handler/adminratelimit.go`). Çok adresi olan bir çağıran, uçuştaki isteği kadar eşzamanlı karşılaştırma koşturabiliyordu; tap yüzeyi aynı süreci paylaşıyor. Bir giriş 1 + `MaxCandidates` = 9 karşılaştırmadır (§5.6).

## 2. Karar

`internal/adminauth/workgate.go`: bir semafor (`WorkGate`). `cmd/tappa` **bir tane** kurar ve bcrypt yapan dört servise verir (`New`, `NewResets`, `NewSignupClaims`, `NewAdminInvites`; `WithWorkGate`). Kapsam: `Authenticate`, `AuthenticatePerson`, `ChangeOwnPassword`, `Unlink` / `Relink`, `Resets.Consume`, `SignupClaims.Open` / `ProveWithPassword` / `ProveWithIdentityPassword`, `AdminInvites.AcceptNew`.

| Değer | Sayı | Neden |
|---|---|---|
| Slot | `WorkSlots()` = `GOMAXPROCS / 2`, en az 1 | bir P tap'e kalır (§4 ölçüm) |
| Prod slot | 1 | `limits.cpu: "2"` (`deploy/k8s/20-app.yaml`, okundu); Go 1.25+ `GOMAXPROCS`'u cgroup sınırından alır (sürüm notu, okundu; pod'da ölçülmedi). Açılış satırı `bcrypt work gate` sayıyı yazar |
| Bekleme | `WorkWait` = 10 sn | sunucuda `WriteTimeout` yok (`cmd/tappa`) |
| Kuyruk | slot başına `WorkWait / signInHold` = 2 | `signInHold` = 9 × 380 ms (paketin sabit güvenlik bütçesi) = 3,42 sn |
| Kaynak başına | **en çok 1 global yer** (çalışan ya da global kuyrukta) + **1 kaynak bekleyeni** (global yer tutmaz) | §3 |

- **Slot bir işlemdir, karşılaştırma değil.** Bir giriş 9 karşılaştırmayı tek slotta sırayla yapar. Bekleyen biri **bütün bir girişi** bekler (9 karşılaştırma, ~2,8–3,7 sn, yükte daha uzun), tek karşılaştırmayı değil.
- **Zamanlama (§5.6 korunur).** Kapı her yöntemin en başında, yazılan adres ya da token okunmadan girilir. Ret `ErrBusy`, iki süre sınıfında, ikisi de yazılan girdiden bağımsız: hemen (kuyruk dolu, ya da kaynağın sırasında zaten iki istek var) ya da toplam `WorkWait` sonunda. Bekleme yalnız çağıranın kendi önceki isteğine ve genel yüke bağlıdır.
- **Cevap.** 503 + `Retry-After: 60`, yüzey başına tek sabit sayfa. Audit satırı yok (hiçbir şey karşılaştırılmadı, tenant bilinmiyor). Kapı dakikada en çok bir WARN yazar, adres ya da kişi içermez. Busy ret, istek önündeki bütçe birimini iade eder: giriş denemesi, kayıt denemesi + create, onay, kayıt linki, sıfırlama gönderimi, davet kabulü. Ret karşılaştırmasızdır, yani iade saldırgana bedava tahmin vermez.

## 3. Kaynak başına adalet

Kaynak = istemci adres kovası, `httpx.RateKey` (IPv4 adresi, IPv6 /64). Her abuse bütçesinin anahtarıyla aynıdır. `httpx.RealIP` her istek için koyar (`adminauth.WithWorkSource`); yazılan e-postadan değil, isteğin geldiği adresten gelir.

- Bir kaynak kapıda en çok **bir global yer** tutar (slot ya da global kuyruk).
- İkinci eşzamanlı isteği **global kuyruğun dışında, kaynağın kendi sırasında** bekler. Bu bekleyen ne slot ne kuyruk yeri tutar ve ilk istek bitince yarışa girer.
- Üçüncüsü hemen `ErrBusy` alır.
- Toplam bekleme `WorkWait` ile sınırlıdır (`TestWorkGate_TheWholeWaitIsBounded`).
- **Neden sıra, neden ret değil** (üçüncü göz, 2026-11-07): aynı kaynağın en sık ikinci isteği NAT değil, tek kişinin çift gönderimidir (çift tıklama, iki sekme, mobil yeniden gönderim; giriş sayfası script yüklemiyor). Tarayıcı son cevabı gösterir. İkinci istek ret alırsa kullanıcı *busy* görür, ama ilk istek sunucuda tamamlanır: oturum açılır, `admin.login.succeeded` yazılır ve çerez kaybolur (akıl yürütme, tarayıcıda ölçülmedi).

- **Kuralsız (denetimde, hesap):** prod'un 1 slot + 8 bekleyeni, kendi doğru parolasıyla 2 adresle slotu ve 3 adresle kuyruğu sürekli dolu tutardı. Bir IPv6 /56 sahibi bunu tek makineden yapardı.
- **Kuralla:** kaynak bekleyeni global yer tutmadığından kapıyı dolu tutmak yine slot başına 1 + 2 = **3 kaynak** ister. Prod'da 3 IPv4 adresi ya da 3 IPv6 /64 yeter; bir /56'da 256 tane /64 vardır.

## 4. Ölçümler

- **Cost-12 karşılaştırma** (`-race`'siz, load ~46–53, 5 örnek): 308–412 ms.
- **Tap boyutunda CPU işi** (2 000 SHA-256), `GOMAXPROCS=2`, 6 sn × 2 tur:

| Arkada bcrypt döngüsü | p99 | max |
|---|---|---|
| 0 | 1,02–1,07 ms | 1,1–1,5 ms |
| 1 | 1,06 ms | 1,1–4,0 ms |
| 2 | 24–32 ms | 30–133 ms |
| 8 | 39–128 ms | 134–195 ms |

- **Kuyruk, 8 karşılaştırma** (1 slot, 8 karşılaştırmalı cost-12 girişler 30 ms arayla × 9, `-race`'siz, load ~70): çalışan 4,47 sn'de, birinci bekleyen 9,08 sn'de, ikinci bekleyen 13,85 sn'de cevap aldı (9,08'de kabul edildi). Kalan altısı 10,1–10,3 sn sonra ret aldı. Yani **2 bekleyen hizmet alıyor**, türetilen kuyrukla aynı.
- **Kuyruk, 9 karşılaştırma** (kimlik kolu; kaynak sırasından sonra; 1 slot, her biri ayrı kaynaktan 9 cost-12 karşılaştırmalık işlem, 30 ms arayla × 5, `-race`'siz, load ~20, iki tur): 1. bekleyen 2,11 / 3,02 sn'de, 2. bekleyen 4,28 / 5,22 sn'de slota girdi. 3. ve 4. hemen ret aldı (kuyruk dolu). Bir işlem 2,1–3,0 sn sürdü.
- **Zamanlama testleri** (CI bayrakları, bu dalda): backlog T86 satırında.

## 5. Kabul edilen risk

**Tap için yük atma takası.** Kapı doluyken şu akışlar **bütün tenant'larda** 503 olur: panel girişi (iki kol), parola sıfırlama gönderimi (`Consume`), kayıt taslağı ve kayıt onayı, yönetici daveti kabulü, parola değişimi, hesap sayfasındaki bağ kopar/geri al. Tap, aktivasyon, panelin oturumlu okuma ve yazmaları, operatör yüzeyi etkilenmez.

- **Kaç kaynakla.** Prod'da aynı anda en az 3 kaynak, her biri kendi isteğini kapıda tutarak. Doğru parola gerekmez: yanlış parolalı giriş de 9 karşılaştırma öder (§5.6), adres başına iş bütçesi `adminLoginWorkLimit` = 106 giriş / 10 dk (okundu). Kaynak başına sürekli bir istek bu bütçenin içinde kalır.
- **Aynı kaynak.** Çift gönderim ve iki sekme artık bekler, 503 almaz. Tek adresin arkasında (ofis NAT'ı) aynı anda iki kişi de bekler: ikincisi birincinin bitmesini bekler. **Üçüncüsü** hemen 503 alır ve tekrar dener.
- **Yavaş veritabanı.** Slot bütün işlem boyunca tutulur: DB gidiş-dönüşleri ve kimlik decoy'unun beklemesi (en çok 250 ms) dahil (`AuthenticatePerson`, `freshIdentitySession`, `ChangeOwnPassword`). 1 slotta yavaş bir DB bütün panel girişlerini kuyruğa sokar.
- **Kurtarma yolu.**
  - Sel, kaynakları bitince ya da adres başına bütçeleri dolunca kendiliğinden biter. Adres bütçeleri `adminFloodLimit` = 3000 / 10 dk, `adminLoginWorkLimit` = 106 / 10 dk (okundu).
  - İnsanın yolu tekrar denemektir (sayfa *"Try again in a minute"* der).
  - Operatörün yolu iki tanedir. Ingress'te kaynak engellemek (bugün yalnız operatör host'u için kısıt var, K4). Ya da pod'a daha çok CPU verip slotu artırmak (`limits.cpu`; replika değil, T1).
  - Kapıyı kapatan bir anahtar yoktur.
- **Kapsam dışı:** operatör yüzeyi (`internal/operatorauth`). Kendi süreç geneli kayıt bütçesi ve adres başına iş bütçesi vardır (ADR 0020); ingress'te adres kısıtı vardır (okundu).

## 6. Ölçülmeyenler

- **Kuyruğun ikinci bekleyeni sınırda.** Prod CPU'sunda cost-12 süresi ölçülmedi. İkinci bekleyen, önündeki iki işlem 10 sn içinde biterse hizmet alır, yani 9 karşılaştırmalık bir işlem en çok ~5 sn sürmeli. Yerelde bu load ~20'de 2,1–3,0 sn ölçüldü. Load ~70'te 8 karşılaştırma 4,47 sn sürdü, 9 karşılaştırma için bu ~5,0 sn eder (oranla hesap): sınır. **Seçim: `WorkWait` = 10 sn ve kuyruk = 2 kalır.**
  - `WorkWait`'i uzatmak her reddedilen isteğin bağlantısını ve bekleme süresini de uzatır.
  - Kuyruğu 1'e indirmek kapıyı doldurmanın maliyetini slot başına 3 kaynaktan 2'ye düşürür.
  - Sınırdaki ikinci bekleyenin bedeli, 10 sn sonra öteki retlerle aynı 503'tür; yanlış bir cevap değildir.
  - Prod'da cost-12 süresi ölçülünce yeniden türetilir.

- Prod pod'unda `GOMAXPROCS`, prod CPU'sunda cost-12 süresi, CFS kısıtlamasının tap'e etkisi.
- Linux CI'da kuyruk süreleri.
- Saldırının kendisi (§3 ve §5'teki kaynak sayıları hesaptır).
- Kaynak kuralının IPv6 /64 dışındaki NAT biçimlerine etkisi.
