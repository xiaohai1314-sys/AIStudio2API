// Package accountenv 校验账户语言、时区与出口 IP 所在地区的一致性
package accountenv

import (
	"fmt"
	"slices"
	"strings"
	"unicode"

	"github.com/Mag1cFall/AIStudio2API/internal/tzcache"
)

// Warning 表示一处环境不一致，Code 供前端识别，Message 面向用户
type Warning struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// Suggestion 表示按地区推导出的建议语言与时区，空字段表示没有建议
type Suggestion struct {
	Locale   string `json:"locale,omitempty"`
	Timezone string `json:"timezone,omitempty"`
}

// countryLanguages 是各国家或地区的主要语言，用于建议语言
var countryLanguages = map[string]string{
	"AE": "ar", "AR": "es", "AT": "de", "AU": "en", "BE": "nl", "BG": "bg",
	"BR": "pt", "CA": "en", "CH": "de", "CL": "es", "CN": "zh", "CO": "es",
	"CZ": "cs", "DE": "de", "DK": "da", "EE": "et", "EG": "ar", "ES": "es",
	"FI": "fi", "FR": "fr", "GB": "en", "GR": "el", "HK": "zh", "HR": "hr",
	"HU": "hu", "ID": "id", "IE": "en", "IL": "he", "IN": "en", "IS": "is",
	"IT": "it", "JP": "ja", "KE": "en", "KR": "ko", "LT": "lt", "LU": "fr",
	"LV": "lv", "MA": "ar", "MX": "es", "MY": "ms", "NL": "nl", "NO": "no",
	"NZ": "en", "PE": "es", "PH": "en", "PL": "pl", "PT": "pt", "RO": "ro",
	"RS": "sr", "RU": "ru", "SA": "ar", "SE": "sv", "SG": "en", "SI": "sl",
	"SK": "sk", "TH": "th", "TR": "tr", "TW": "zh", "UA": "uk", "US": "en",
	"VN": "vi", "ZA": "en",
}

// countryZoneOverrides 修正生成表的代表时区：生成表取 zone1970.tab 中首个列出该国的时区，
// 会选到离岛或他国时区（如 AU 选到 Lord_Howe、UA 选到 Simferopol），这里改为各国的主流主时区
var countryZoneOverrides = map[string]string{
	"AU": "Australia/Sydney",
	"BR": "America/Sao_Paulo",
	"CA": "America/Toronto",
	"DE": "Europe/Berlin",
	"DK": "Europe/Copenhagen",
	"IS": "Atlantic/Reykjavik",
	"NL": "Europe/Amsterdam",
	"NO": "Europe/Oslo",
	"RU": "Europe/Moscow",
	"SE": "Europe/Stockholm",
	"UA": "Europe/Kyiv",
}

// representativeZone 返回国家或地区的代表时区
func representativeZone(country string) (string, bool) {
	if zone, ok := countryZoneOverrides[country]; ok {
		return zone, true
	}
	zone, ok := countryZones[country]
	return zone, ok
}

// Check 校验语言与时区的自洽性，返回警告与建议；不涉及网络
func Check(locale, timezone string) ([]Warning, Suggestion) {
	warnings := make([]Warning, 0, 2)
	var suggestion Suggestion
	language, region, localeValid := SplitLocale(locale)
	if !localeValid {
		warnings = append(warnings, Warning{Code: "invalid_locale", Message: "语言格式无效，应写成 语言-地区，例如 en-US"})
	} else if region == "" {
		warnings = append(warnings, Warning{Code: "locale_missing_region", Message: "语言缺少地区部分，例如应为 en-US 而不是 en"})
	}
	zone := strings.TrimSpace(timezone)
	countries, zoneValid := ZoneCountries(zone)
	if !zoneValid {
		warnings = append(warnings, Warning{Code: "invalid_timezone", Message: "时区不是合法的 IANA 名称，例如 Asia/Shanghai 或 America/Los_Angeles"})
	}
	if zoneValid && region != "" && len(countries) > 0 && !slices.Contains(countries, region) {
		warnings = append(warnings, Warning{
			Code:    "locale_timezone_mismatch",
			Message: fmt.Sprintf("语言地区 %s 与时区 %s（%s）不一致", region, zone, strings.Join(countries, "/")),
		})
		if representative, ok := representativeZone(region); ok {
			suggestion.Timezone = representative
		}
		if len(countries) == 1 {
			suggestion.Locale = suggestLocale(language, countries[0])
		}
	}
	if !zoneValid && region != "" {
		if representative, ok := representativeZone(region); ok {
			suggestion.Timezone = representative
		}
	}
	if localeValid && region == "" && zoneValid && len(countries) == 1 {
		suggestion.Locale = language + "-" + countries[0]
	}
	return warnings, suggestion
}

// CheckExit 在 Check 的基础上比对出口 IP 所在国家，返回警告与建议
func CheckExit(locale, timezone, country string) ([]Warning, Suggestion) {
	warnings, suggestion := Check(locale, timezone)
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return warnings, suggestion
	}
	language, region, localeValid := SplitLocale(locale)
	if localeValid && region != "" && region != country {
		warnings = append(warnings, Warning{
			Code:    "locale_exit_mismatch",
			Message: fmt.Sprintf("出口 IP 位于 %s，与语言地区 %s 不一致", country, region),
		})
	}
	if countries, ok := ZoneCountries(timezone); ok && len(countries) > 0 && !slices.Contains(countries, country) {
		warnings = append(warnings, Warning{
			Code:    "timezone_exit_mismatch",
			Message: fmt.Sprintf("出口 IP 位于 %s，与时区 %s（%s）不一致", country, strings.TrimSpace(timezone), strings.Join(countries, "/")),
		})
	}
	// 当前时区已与出口国家相符时不要给改时区的建议：代表时区只是该国的某一个时区，
	// 无条件覆盖会把本来正确的时区（如 America/Chicago）诱导改成另一个（如 America/New_York）
	zoneFits := false
	if countries, ok := ZoneCountries(timezone); ok && slices.Contains(countries, country) {
		zoneFits = true
	}
	if representative, ok := representativeZone(country); ok && !zoneFits {
		suggestion.Timezone = representative
	}
	suggestion.Locale = suggestLocale(language, country)
	return warnings, suggestion
}

// SuggestForCountry 返回国家或地区的建议语言与时区，未收录时返回空
func SuggestForCountry(country string) (locale, timezone string) {
	country = strings.ToUpper(strings.TrimSpace(country))
	if country == "" {
		return "", ""
	}
	zone, _ := representativeZone(country)
	return suggestLocale("", country), zone
}

// SplitLocale 拆分语言与地区，语言小写、地区大写；地区可省略，格式无效时 valid 为 false
// 接受 BCP 47 的脚本子标签（如 zh-Hans-CN、sr-Latn-RS），地区取脚本之后的段
func SplitLocale(locale string) (language, region string, valid bool) {
	locale = strings.TrimSpace(locale)
	if locale == "" {
		return "", "", false
	}
	parts := strings.Split(strings.ReplaceAll(locale, "_", "-"), "-")
	language = strings.ToLower(parts[0])
	if len(language) < 2 || len(language) > 3 || !letters(language) {
		return "", "", false
	}
	if len(parts) == 1 {
		return language, "", true
	}
	rest := parts[1:]
	if len(rest) > 1 && len(rest[0]) == 4 && letters(rest[0]) {
		rest = rest[1:]
	}
	if len(rest) != 1 {
		return "", "", false
	}
	region = strings.ToUpper(rest[0])
	if len(region) == 2 && letters(region) || len(region) == 3 && digits(region) {
		return language, region, true
	}
	return "", "", false
}

// zoneAliases 是 tzdb 的向后兼容链接名到规范时区名的映射：
// 别名能被 LoadLocation 加载，但生成表里只有规范名，不映射就会查空集合、整段一致性检查被跳过
var zoneAliases = map[string]string{
	"America/Godthab":      "America/Nuuk",
	"America/Indianapolis": "America/Indiana/Indianapolis",
	"America/Montreal":     "America/Toronto",
	"Asia/Calcutta":        "Asia/Kolkata",
	"Asia/Chongqing":       "Asia/Shanghai",
	"Asia/Chungking":       "Asia/Shanghai",
	"Asia/Harbin":          "Asia/Shanghai",
	"Asia/Istanbul":        "Europe/Istanbul",
	"Asia/Katmandu":        "Asia/Kathmandu",
	"Asia/Saigon":          "Asia/Ho_Chi_Minh",
	"Canada/Atlantic":      "America/Halifax",
	"Canada/Central":       "America/Winnipeg",
	"Canada/Eastern":       "America/Toronto",
	"Canada/Mountain":      "America/Edmonton",
	"Canada/Newfoundland":  "America/St_Johns",
	"Canada/Pacific":       "America/Vancouver",
	"Europe/Belfast":       "Europe/London",
	"Europe/Kiev":          "Europe/Kyiv",
	"US/Alaska":            "America/Anchorage",
	"US/Central":           "America/Chicago",
	"US/Eastern":           "America/New_York",
	"US/Hawaii":            "Pacific/Honolulu",
	"US/Mountain":          "America/Denver",
	"US/Pacific":           "America/Los_Angeles",
}

// ZoneCountries 返回时区使用的国家或地区集合，时区非法时 valid 为 false；UTC 等无国家时区返回空集合
func ZoneCountries(timezone string) ([]string, bool) {
	timezone = strings.TrimSpace(timezone)
	if timezone == "" {
		return nil, false
	}
	location, err := tzcache.Load(timezone)
	if err != nil {
		// 本地 tzdata 可能没有别名的链接条目（如裁剪过的镜像缺 Europe/Kiev），
		// 已知别名改用规范名重试，避免把合法时区误报为无效
		canonical, ok := zoneAliases[timezone]
		if !ok {
			return nil, false
		}
		location, err = tzcache.Load(canonical)
		if err != nil {
			return nil, false
		}
	}
	name := location.String()
	if canonical, ok := zoneAliases[name]; ok {
		name = canonical
	}
	return zoneCountries[name], true
}

// suggestLocale 按国家的主要语言给出语言-地区，主要语言缺失时沿用当前语言
func suggestLocale(language, country string) string {
	primary := countryLanguages[country]
	if primary == "" || primary == language {
		if language == "" {
			primary = "en"
		} else {
			primary = language
		}
	}
	return primary + "-" + country
}

// letters 表示全部为英文字母
func letters(value string) bool {
	for _, r := range value {
		if !unicode.IsLetter(r) || r > unicode.MaxASCII {
			return false
		}
	}
	return value != ""
}

// digits 表示全部为数字
func digits(value string) bool {
	for _, r := range value {
		if r < '0' || r > '9' {
			return false
		}
	}
	return value != ""
}
