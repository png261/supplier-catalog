# Tài Liệu Kịch Bản & Thuyết Minh Video Demo (Tiếng Việt)
## Assessment 1: Supplier Catalog Updates (Hệ Thống Đồng Bộ Danh Mục Bền Bỉ Bằng Go)

> **Tệp Video Demo Hoàn Chỉnh**: [`demo_walkthrough_vi.mp4`](file:///Users/png/01.%20Project/coderpush/demo_walkthrough_vi.mp4) *(Độ phân giải 1080p, chuẩn H.264 / AAC, dung lượng: ~4.6 MB)*  
> **Bộ Giọng Đọc AI**: [**VieNeu-TTS (v3 Turbo)**](https://github.com/pnnbao97/VieNeu-TTS) *(Preset Voice: "Hải Đăng" - Nam · Bắc · Phong cách tự nhiên)*  
> **Kho lưu trữ GitHub**: [https://github.com/png261/supplier-catalog](https://github.com/png261/supplier-catalog)  
> **Gói Docker Container (GHCR)**: `ghcr.io/png261/supplier-catalog:latest`

---

### Cấu Trúc Toàn Bộ 8 Phân Cảnh Tính Năng

```
+-------------------------------------------------------------------------------------------------------+
| PHÂN CẢNH 01 [0:00 - 0:26]  Tổng Quan Kiến Trúc & Sứ Mệnh Kỹ Thuật (Pure Go + SQLite WAL)             |
| PHÂN CẢNH 02 [0:26 - 0:56]  5 Quy Tắc Toán Học Bất Biến Về Dữ Liệu (Invariants A, B, C, D, E)         |
| PHÂN CẢNH 03 [0:56 - 1:22]  Công Cụ Phát Sinh Dữ Liệu Giả Lập & Bơm Lỗi (128-bit PCG PRNG)            |
| PHÂN CẢNH 04 [1:22 - 1:43]  Web Dashboard Tối Giản Nhúng Trực Tiếp (Zero-Node SPA & REST Control)     |
| PHÂN CẢNH 05 [1:43 - 2:03]  Đồng Bộ Dữ Liệu Thời Gian Thực & Luồng Sự Kiện Server-Sent Events (SSE)   |
| PHÂN CẢNH 06 [2:03 - 2:23]  Tra Cứu Danh Mục Sản Phẩm & Hàng Đợi Lỗi (Dead-Letter Queue Isolation)    |
| PHÂN CẢNH 07 [2:23 - 2:45]  Khả Năng Tự Phục Hồi Sau Crash (SIGKILL) & Resumption Checkpoint (AC1&AC2)|
| PHÂN CẢNH 08 [2:45 - 3:12]  Kiểm Định Chất Lượng (93.5% Coverage, 0 Races), Container 21MB & CI/CD    |
+-------------------------------------------------------------------------------------------------------+
```

---

### Lời Thuyết Minh Chi Tiết Từng Phân Cảnh

#### Phân Cảnh 1: Tổng Quan Kiến Trúc Hệ Thống (0:00 – 0:26)
- **Hình ảnh trình chiếu**: Slide tiêu đề tối giản phong cách Dieter Rams/Swiss hiển thị 3 trụ cột kỹ thuật: Invariants toán học tất định, Kiến trúc SQLite WAL không phụ thuộc message broker, Cổng kiểm soát chất lượng chuẩn production.
- **Lời thoại thuyết minh**:
  > *"Chào mừng bạn đến với video trình diễn kỹ thuật hệ thống Đồng Bộ Danh Mục Nhà Cung Cấp. Được xây dựng hoàn toàn bằng ngôn ngữ Go và cơ sở dữ liệu SQLite ở chế độ Write-Ahead Log, hệ thống mang lại khả năng đồng bộ dữ liệu tất định dưới các điều kiện mạng bị xáo trộn, trùng lặp hoặc mất kết nối, mà không cần phụ thuộc vào các message broker cồng kềnh bên ngoài."*

#### Phân Cảnh 2: 5 Invariants Toán Học Cốt Lõi (0:26 – 0:56)
- **Hình ảnh trình chiếu**: Sơ đồ trực quan 5 quy tắc bất biến (A – E):
  - **Invariant A**: Idempotency (chống trùng lặp qua content hash).
  - **Invariant B**: Fast-Forward Timestamps (chỉ ghi nhận bản ghi mới, bỏ qua bản ghi cũ).
  - **Invariant C**: Tombstone Preservation (sản phẩm đã WITHDRAWN là vĩnh viễn, không thể hồi sinh).
  - **Invariant D**: Dead-Letter Queue (cách ly bản ghi sai schema mà không nghẽn luồng).
  - **Invariant E**: Atomic Batch Commit & Resumption (giao dịch nguyên tử qua SQLite WAL).
- **Lời thoại thuyết minh**:
  > *"Trái tim của hệ thống là năm quy tắc toán học bất biến: Invariant A đảm bảo tính lũy đẳng qua cơ chế so khớp hash nội dung. Invariant B tự động loại bỏ bản ghi cũ và cập nhật bản ghi mới hơn. Invariant C bảo toàn trạng thái thu hồi sản phẩm vĩnh viễn. Invariant D cách ly bản ghi sai định dạng vào hàng đợi lỗi dead-letter queue, và Invariant E cam kết commit nguyên tử từng batch giao dịch."*

#### Phân Cảnh 3: Công Cụ Tạo Dữ Liệu Giả Lập & Bơm Lỗi (0:56 – 1:22)
- **Hình ảnh trình chiếu**: Bảng thông số kỹ thuật bộ sinh dữ liệu CLI (`cmd/generator`): PRNG 128-bit PCG, ring buffer $O(1)$ lưu vết SKU, cùng 5 tỷ lệ bơm bất thường (update, duplicate, out-of-order, withdrawn, invalid).
- **Lời thoại thuyết minh**:
  > *"Để phục vụ việc kiểm thử, chúng tôi xây dựng công cụ phát sinh dữ liệu giả lập sử dụng thuật toán PCG 128 bit. Công cụ này duy trì một ring buffer với độ phức tạp O 1, cho phép bơm có kiểm soát các bất thường như bản ghi trùng lặp, bản ghi đến muộn, sản phẩm bị thu hồi hoặc dữ liệu sai giá, giúp kiểm thử sức chịu đựng của toàn bộ hệ thống."*

#### Phân Cảnh 4: Giao Diện Dashboard Nhúng Trực Tiếp (1:22 – 1:43)
- **Hình ảnh trình chiếu**: Chụp màn hình thực tế Web Dashboard khi máy chủ khởi động ở trạng thái sẵn sàng, menu điều khiển REST, bảng thống kê và khu vực kết nối SSE.
- **Lời thoại thuyết minh**:
  > *"Đây là giao diện web dashboard tối giản theo phong cách Dieter Rams, được nhúng thẳng vào file nhị phân của Go qua tính năng go embed mà không cần môi trường Nodejs. Giao diện cung cấp bảng điều khiển REST API và kết nối trực tiếp với luồng sự kiện Server-Sent Events của máy chủ."*

#### Phân Cảnh 5: Đồng Bộ Trực Tiếp & Server-Sent Events (1:43 – 2:03)
- **Hình ảnh trình chiếu**: Chụp màn hình thực tế Web Dashboard khi đang nhận dữ liệu streaming: thanh tiến trình cập nhật real-time, các thẻ số liệu Added, Updated, Withdrawn, Skipped, Dead-Lettered nhảy số trực tiếp qua SSE.
- **Lời thoại thuyết minh**:
  > *"Khi một tiến trình đồng bộ được kích hoạt, máy chủ đẩy trực tiếp tiến độ, chỉ số batch và thời gian phản hồi về trình duyệt qua Server-Sent Events theo thời gian thực. Cơ chế quản lý áp lực luồng đảm bảo khách hàng đọc chậm không thể làm nghẽn hoạt động phân phối sự kiện của hệ thống."*

#### Phân Cảnh 6: Tra Cứu Danh Mục & Dead-Letter Queue (2:03 – 2:23)
- **Hình ảnh trình chiếu**: Bảng dữ liệu hai cột: bên trái là danh mục sản phẩm (ACTIVE / WITHDRAWN kèm giá và timestamp), bên phải là Dead-Letter Queue hiển thị mã lỗi (INVALID_PRICE, MISSING_SKU) và lý do từ chối.
- **Lời thoại thuyết minh**:
  > *"Hệ thống cung cấp giao diện tra cứu danh mục sản phẩm hoàn chỉnh cùng hàng đợi dead-letter queue. Các bản ghi lỗi như giá âm hoặc thiếu mã SKU được lưu vết chi tiết kèm mã lỗi và lý do từ chối, giúp đội ngũ vận hành dễ dàng kiểm tra mà không làm gián đoạn luồng xử lý chính."*

#### Phân Cảnh 7: Khả Năng Tự Phục Hồi Sau Khi Crash (2:23 – 2:45)
- **Hình ảnh trình chiếu**: Mô phỏng màn hình Terminal khi tiến trình đang xử lý dở dang thì bị dập tắt bởi `SIGKILL`. Khi máy chủ khởi động lại, startup hook quét sạch WAL dở dang và resume chính xác từ checkpoint cuối cùng.
- **Lời thoại thuyết minh**:
  > *"Khả năng phục hồi khi gặp sự cố là điểm mạnh cốt lõi của giải pháp. Khi tiến trình bị tắt đột ngột do sập nguồn, hook khởi động của hệ thống sẽ tự động quét log SQLite WAL, hủy các frame dở dang và tiếp tục xử lý chính xác từ checkpoint cuối cùng đã commit, cam kết không ghi đúp dữ liệu."*

#### Phân Cảnh 8: Kiểm Định Chất Lượng, Docker & CI/CD (2:45 – 3:12)
- **Hình ảnh trình chiếu**: Bảng số liệu nghiệm thu thực tế: Test coverage **93.5%**, **0 Race conditions**, Container **21.3MB**, GitHub Actions CI/CD **100% Green** xuất bản lên GitHub Container Registry (GHCR).
- **Lời thoại thuyết minh**:
  > *"Dự án được bảo chứng bởi các cổng kiểm soát chất lượng tự động: không rò rỉ mã bí mật, không có xung đột luồng qua năm lần kiểm tra race condition, và độ bao phủ kiểm thử đạt chín mươi ba phẩy năm phần trăm. Ứng dụng được đóng gói thành container Docker siêu nhẹ hai mươi mốt megabyte và tự động xuất bản lên GitHub Container Registry. Xin cảm ơn bạn đã theo dõi."*

---

### Hướng Dẫn Mở Hoặc Tạo Lại Video

#### 1. Mở xem video ngay trên máy Mac:
```bash
open "/Users/png/01. Project/coderpush/demo_walkthrough_vi.mp4"
```

#### 2. Tự động sinh lại video với VieNeu-TTS:
```bash
/tmp/vieneu-venv/bin/python scripts/generate_demo_vi.py
```
*(Chạy trực tiếp với mô hình VieNeu-TTS v3 Turbo ONNX Lite engine, giọng đọc mẫu "Hải Đăng" cực kỳ tự nhiên, hỗ trợ chuyển đổi mã song ngữ Việt - Anh mượt mà).*
