package accountenv

import (
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/oschwald/maxminddb-golang"
)

// ExitIPEchoURL 是经代理访问、返回出口 IP 的回显服务
const ExitIPEchoURL = "https://api64.ipify.org"

// GeoIP 读取 MaxMind 兼容格式的国家数据库
type GeoIP struct {
	reader *maxminddb.Reader
}

// geoIPRecord 是数据库中的国家字段
type geoIPRecord struct {
	Country struct {
		ISO string `maxminddb:"iso_code"`
	} `maxminddb:"country"`
}

// OpenGeoIP 打开国家数据库，DB-IP 免费库与 GeoLite2 均为该格式
func OpenGeoIP(path string) (*GeoIP, error) {
	reader, err := maxminddb.Open(path)
	if err != nil {
		return nil, fmt.Errorf("打开 GeoIP 数据库: %w", err)
	}
	return &GeoIP{reader: reader}, nil
}

// Close 关闭数据库
func (geoip *GeoIP) Close() error {
	return geoip.reader.Close()
}

// Country 返回 IP 所在国家或地区代码，未收录时为空
func (geoip *GeoIP) Country(ip netip.Addr) (string, error) {
	var record geoIPRecord
	if err := geoip.reader.Lookup(net.IP(ip.Unmap().AsSlice()), &record); err != nil {
		return "", fmt.Errorf("查询 GeoIP: %w", err)
	}
	return record.Country.ISO, nil
}

// ExitIP 经 client 访问回显服务，返回代理的出口 IP
func ExitIP(ctx context.Context, client *http.Client) (netip.Addr, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, ExitIPEchoURL, nil)
	if err != nil {
		return netip.Addr{}, err
	}
	response, err := client.Do(request)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("查询出口 IP: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return netip.Addr{}, fmt.Errorf("查询出口 IP 返回 HTTP %d", response.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, 64))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("读取出口 IP: %w", err)
	}
	address, err := netip.ParseAddr(strings.TrimSpace(string(body)))
	if err != nil {
		return netip.Addr{}, fmt.Errorf("出口 IP 格式无效: %w", err)
	}
	return address, nil
}

// DownloadGeoIP 下载 DB-IP 免费国家库并解压保存到 path，当月文件未发布时回退到上月
func DownloadGeoIP(ctx context.Context, client *http.Client, path string, now time.Time) error {
	// 上月从当月 1 号倒推，避免 AddDate 在月末被归一化回当月（如 3 月 31 日减一月得到 3 月 3 日）
	firstOfMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	var lastErr error
	for _, month := range []time.Time{now, firstOfMonth.AddDate(0, -1, 0)} {
		if err := downloadGeoIPMonth(ctx, client, path, month); err == nil {
			return nil
		} else {
			lastErr = err
		}
	}
	return lastErr
}

// maxGeoIPDatabaseBytes 是解压后数据库的大小上限：国家库正常在数 MiB 量级，
// 上限用于挡住高压缩比文件把磁盘写满
const maxGeoIPDatabaseBytes int64 = 128 << 20

// downloadGeoIPMonth 下载指定月份的国家库，先写临时文件、校验是合法 MMDB 再替换，避免半成品或坏文件被读取
func downloadGeoIPMonth(ctx context.Context, client *http.Client, path string, month time.Time) error {
	address := "https://download.db-ip.com/free/dbip-country-lite-" + month.Format("2006-01") + ".mmdb.gz"
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("下载 GeoIP 数据库: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("下载 GeoIP 数据库返回 HTTP %d", response.StatusCode)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), ".dbip-*.mmdb")
	if err != nil {
		return err
	}
	defer os.Remove(temporary.Name())
	reader, err := gzip.NewReader(response.Body)
	if err != nil {
		temporary.Close()
		return fmt.Errorf("解压 GeoIP 数据库: %w", err)
	}
	written, err := io.Copy(temporary, io.LimitReader(reader, maxGeoIPDatabaseBytes+1))
	if err != nil {
		temporary.Close()
		return fmt.Errorf("写入 GeoIP 数据库: %w", err)
	}
	if written > maxGeoIPDatabaseBytes {
		temporary.Close()
		return fmt.Errorf("GeoIP 数据库超过大小上限 %d MiB", maxGeoIPDatabaseBytes>>20)
	}
	if err := temporary.Close(); err != nil {
		return err
	}
	// 替换前先按 MMDB 打开并校验内容：上游返回合法 gzip 包裹的非数据库内容时，
	// 不能把已可用的数据库换成坏文件、让 GeoIP 功能静默失效
	probe, err := maxminddb.Open(temporary.Name())
	if err != nil {
		return fmt.Errorf("校验 GeoIP 数据库: %w", err)
	}
	verifyErr := probe.Verify()
	closeErr := probe.Close()
	if verifyErr != nil {
		return fmt.Errorf("校验 GeoIP 数据库: %w", verifyErr)
	}
	if closeErr != nil {
		return closeErr
	}
	// Windows 上目标文件可能正被并发的出口查询以 mmap 方式打开，替换会短暂失败，重试几次再放弃；
	// 放弃时旧数据库保持不变，下次更新仍可成功
	renameErr := os.Rename(temporary.Name(), path)
	for attempt := 0; renameErr != nil && attempt < 10; attempt++ {
		time.Sleep(200 * time.Millisecond)
		renameErr = os.Rename(temporary.Name(), path)
	}
	if renameErr != nil {
		return fmt.Errorf("替换 GeoIP 数据库: %w", renameErr)
	}
	return nil
}
