package api

import (
	"encoding/json"
	"testing"
)

// silencePCM 生成指定帧数的 16-bit 单声道静音 PCM
func silencePCM(frames, rate int) audioPCM {
	return audioPCM{Data: make([]byte, frames*2), SampleRate: rate, Channels: 1}
}

// TestEncodeAudioRejectsExcessiveSampleRate 验证未钳制采样率的格式（pcm/alaw/mulaw）
// 在重采样分配前拒绝异常采样率，而不是按输出帧数尝试超大分配
func TestEncodeAudioRejectsExcessiveSampleRate(t *testing.T) {
	source := silencePCM(480, 48000)
	for _, rate := range []int{500_000_000, maxAudioSampleRate + 1, -5} {
		for _, format := range []string{"pcm", "alaw", "mulaw"} {
			if _, _, err := encodeAudio(source, audioOutput{Format: format, SampleRate: rate}); err == nil {
				t.Errorf("format=%s sample_rate=%d 时应报错", format, rate)
			}
		}
	}
}

// TestEncodeAudioResampleWithinLimits 验证合法采样率的重采样照常工作、输出帧数正确
func TestEncodeAudioResampleWithinLimits(t *testing.T) {
	data, encoded, err := encodeAudio(silencePCM(480, 48000), audioOutput{Format: "pcm", SampleRate: 24000})
	if err != nil {
		t.Fatalf("合法重采样不应报错: %v", err)
	}
	if encoded.SampleRate != 24000 {
		t.Fatalf("编码采样率 = %d, 期望 24000", encoded.SampleRate)
	}
	if len(data) != 240*2 {
		t.Fatalf("输出正文 = %d 字节, 期望 480（240 帧单声道 16-bit）", len(data))
	}
}

// TestGeminiAudioFormatRejectsExcessiveSampleRate 验证 Gemini 入口同样拒绝超限采样率
func TestGeminiAudioFormatRejectsExcessiveSampleRate(t *testing.T) {
	var config geminiGenerationConfig
	if err := json.Unmarshal([]byte(`{"responseFormat":[{"audio":{"mimeType":"AUDIO_WAV","sampleRate":500000000}}]}`), &config); err != nil {
		t.Fatalf("解析测试配置失败: %v", err)
	}
	if _, err := config.audioFormat(); err == nil {
		t.Fatal("sampleRate=500000000 时应报错")
	}
	var okConfig geminiGenerationConfig
	if err := json.Unmarshal([]byte(`{"responseFormat":[{"audio":{"mimeType":"AUDIO_WAV","sampleRate":24000}}]}`), &okConfig); err != nil {
		t.Fatalf("解析合法配置失败: %v", err)
	}
	format, err := okConfig.audioFormat()
	if err != nil || format == nil || format.SampleRate != 24000 {
		t.Fatalf("合法 sampleRate 应通过: format=%+v err=%v", format, err)
	}
}
