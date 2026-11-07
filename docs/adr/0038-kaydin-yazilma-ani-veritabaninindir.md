# ADR 0038 — Kaydın yazılma anı ve kimliği veritabanınındır (`transactions.created_at`, `id`)

- **Durum:** Kabul edildi (2026-11-07) — backlog **T163**, migration **00088**.
- **Tarih:** 2026-11-07
- **İlgili:** ADR 0006 (debounce sunucu zamanı) · ADR 0032 §A.6 (`W`, "eşzamanlı yazım") · CLAUDE.md §4.3, §4.5, §5, §6
- **Ölçüldü** = `tappa-m14-dbprivs-db`, PostgreSQL 17.10, goose 87→88, 2026-11-07. **Okundu** = çalıştırılmadı.

## Bağlam

İki okuyucu `transactions.created_at`'i *veritabanının* "bu satır ne zaman yazıldı" cevabı sayar:
debounce'un sunucu bacağı (ADR 0006, `SecondsSinceLastRecordedTap`: `clock_timestamp() − created_at`,
`ORDER BY created_at`) ve M15'in "sonradan yazıldı" `W`'si (ADR 0032 §A.6). 00068'in karar
tetikleyicisi de onay satırını onunla sınırlar. Ama `tappa_app` 00005'ten beri `transactions` üzerinde
**tablo düzeyi INSERT** tutuyordu (ölçüldü: `relacl` `tappa_app=ar`), yani `created_at`'i de yazabiliyordu.
Bugün hiçbir üretim sorgusu yazmıyor (okundu: `db/queries`'teki dört INSERT); ADR 0032 §A.6 bunu bir
kaynak taramasıyla pinledi ve onu kendisi *"ağ, kilit değil"* diye adlandırdı. SQL düzeyinde bir ele
geçirme, öncülün `created_at`'ini geriye tarihleyip 60 sn frenini aşabilirdi.

`id` aynı türden bir sütundur: değeri veritabanı çeker (`DEFAULT gen_random_uuid()`), hiçbir üretim
sorgusu adını anmaz (okundu), ve **sıralamaya girer**: yön zinciri ve çıkış sınıflandırması aynı
`occurred_at`'teki iki satırı `(occurred_at, id)` ile ayırır (okundu: `db/queries/anomalies.sql`,
ör. `:735`, `:916`). `id` seçebilen bir çağıran, eş anlı iki satırdan hangisinin "sonra" olduğunu
seçerdi; bir de sızan ya da tahmin edilen bir id üzerinde `transactions_pkey` 23505'iyle kiracılar
arası bir varlık kâhini kazanırdı.

## Karar

`tappa_app`'in `transactions` INSERT'i **sütun listesi** olur: `id` ve `created_at` dışındaki her sütun
(bugün 30 sütun — `InsertTransaction`'ın birebir listesi). Tablo düzeyi SELECT kalır (her INSERT
`id, created_at` döndürür). `created_at`'in değeri **değişmez**: `DEFAULT now()` (işlem başlangıcı).
Bu, değeri **verilemez** kılar; kilidin tamamı değildir (bkz. "Bedel", ilk madde).

**Sonuç kuralı (§6'ya ek):** `transactions`'a eklenen yeni sütun ya **aynı migration'da**
`GRANT INSERT (<sütun>) ON transactions TO tappa_app` alır ya da bilinçli olarak dışlama listesine
(`internal/db/createdatgrant_test.go`'nun `cgAllColumnsBut`'ı ve 00088'in DO bloğu) girer; tablo
düzeyi INSERT geri verilmez. Unutulursa tap'in INSERT'i 42501 olur — güvenli yönde ve CI'da: her checkin DB testi o sorguyu
koşar, `internal/db/createdatgrant_test.go` listeyi "tablo eksi (`id`, `created_at`)"a tutar ve
`db/queries`'teki her INSERT'in adlandırdığı sütunu katalogla karşılaştırır.

## Değerlendirilen seçenekler

| Seçenek | Neden seçilmedi / seçildi |
|---|---|
| **(a) Sütun GRANT'i** | **Seçildi.** Yüksek sesle kırılır (42501), §6 desenidir (`tappa_feedwriter` 00049, `tappa_holdingdefiner` 00046 ve 00068'in iki tablosu doğuştan böyle), sahibin geçmiş tarihli fikstür yazmasına izin verir. |
| **(b) `created_at := now()` BEFORE INSERT tetikleyicisi** | Değeri **sessizce** ezer: 17 test dosyasında 25 fikstür INSERT'i bilerek geçmiş `created_at` yazıyor (ölçüldü: 00088'den sonra 42501 verenler); tetikleyici altında koşmaya devam edip artık sahip olmadıkları bir satır şekli üzerine assert ederlerdi. Sahibi de bağlar, yani yaşlı bir satır kurmanın dürüst bir yolu kalmazdı. `now()` ile dürüst yazara (a)'dan fazla bir şey vermez; her tap INSERT'ine bir satır tetikleyicisi ekler. |
| **(b′) aynı tetikleyici, `clock_timestamp()`** | Debounce'un DB-yaşı bacağının ölçtüğünü değiştirir (ADR 0006'nın *"ölçüldü, yapılmadı, yazıldı"* notu: kilidi W sn bekleyen satırın yaşı W kadar şişer) ve tek işlemdeki satırların `created_at` eşitliğini bozar (00068'in `>=` sınırı, M15'in `W` sıralaması). Bu kart bir davranış değişikliği değil. Ayrı bir karar olarak açık kalır. |
| **(c) yalnız kaynak taraması (bugünkü)** | ADR 0032 §A.6'nın kendi sözüyle ağ: başka dosyada kurulan bir tablo adı onu aşar. Kalır — `SECURITY DEFINER` gövdeleri sahibin haklarıyla koştuğu için oradaki yazarı GRANT değil o tarama yakalar. |

## Eski binary uyumu (expand/contract)

Migrate Job rollout'tan önce, eski pod servis verirken koşar. `db/queries` `main` (`9b4aacbe`) ile bu dalda
aynıdır (okundu: `git diff origin/main -- db/ internal/store` boş). Dört INSERT'in hiçbiri `id` ya da
`created_at` adlandırmaz; hepsi yalnız verilen sütunları yazar (`TestTransactions00088_EveryShippedInsertNamesOnlyGrantedColumns`).
Hiçbir fonksiyon gövdesi `transactions`'a INSERT etmez (okundu: migration'lar). Eski binary yeni şemada
kırılmaz; Down 00005'in tablo düzeyi INSERT'ini geri verir.

## Kilit

GRANT ve REVOKE `transactions` üzerinde kilit almaz (ölçüldü: işlem içinde `pg_locks` ilişki için boş;
başka bir işlem ACCESS EXCLUSIVE tutarken ikisi de < 5 ms'de döndü, aynı `lock_timeout` altında bir
SELECT 55P03 aldı). `lock_timeout = '1s'` sıcak tablo kuralı gereği yine ilk satırdır (00066, 00087).

## Bedel

- **Kalan risk (güvenlik denetimi, 2026-11-07):** `created_at` artık değer olarak verilemez; ama yazan,
  işlemi açık tuttuğu süre kadar işlemin başladığı ana geri çekebilir (`DEFAULT now()` işlem başıdır).
  Denetimde `tappa_app` olarak `BEGIN; pg_sleep(62); INSERT …; ROLLBACK`: yazıldığı andaki yaş
  00:01:02.31, debounce bacağı 0 satır okudu; `pg_sleep` sırasında bağlantı *idle* sayılmadığı için
  30 sn'lik `idle_in_transaction_session_timeout` devreye girmiyor, ana havuzda `statement_timeout` yok.
  Etki kritik değil (aynı yetkideki saldırgan zaten `verdict='ok'` yazabiliyor); kapatmanın yolu (b′)
  `clock_timestamp()`, ayrı bir karar olarak açık — backlog **T226**; ADR 0006 bu dürüst yolu zaten yazıyor.
- **Eski bir migration'ın Down/Up'ını tek başına yürüyen testler** (00008, 00043, 00064, 00065, 00067
  sınıfı) `transactions`'a bir sütunu düşürüp geri ekliyorsa, 00088'i sarmalamadan (önce Down'ı, sonda
  Up'ı) koşarsa kırılır: geri eklenen sütun sütun listesinde değildir. goose'un kendi sırasında sorun
  yok (00088'in Down'ı önce koşar). 00065'in testi düzeltildi (`2f5fc987`); 00064'ünki bugün yeşil.
- Fikstürler: geçmiş `created_at` yazan 25 INSERT sahibin bağlantısına (kiracı bağlamıyla — punch feed
  tetikleyicisi bağlamı ister) taşındı; `id` adlandıran 12 INSERT `RETURNING id`'ye (RLS testleri
  `tappa_app` olarak kalır — `WITH CHECK` sınamasının sebebi yetki değil RLS olmalı) ya da id sırasını
  ölçen bir yerde sahibe geçti.
- ADR 0032 §A.6'nın *"kolon düzeyinde INSERT yetkisi de her yeni sütunda bir GRANT bakımı ister"*
  cümlesi doğru kalır; bakım yukarıdaki sonuç kuralıdır ve CI onu kırmızıyla hatırlatır.
