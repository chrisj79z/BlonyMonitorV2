package app

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"blonymonitorv2/internal/config"
)

const battleUploadTimeout = 15 * time.Second

func shouldUploadBattle(saveName string) bool {
	enabled, endpoint, keyword := getUploadFilterConfig()
	if !enabled || endpoint == "" || keyword == "" || !isUploadSecretConfigured() {
		return false
	}
	return strings.Contains(saveName, keyword)
}

func filterSaveDataForUpload(data SaveFileData) SaveFileData {
	filtered := make([]targetExport, 0, len(data.Targets))
	for _, target := range data.Targets {
		if target.BossHP == nil || target.BossHP.MaxHP < config.MinUploadTargetMaxHP {
			continue
		}
		filtered = append(filtered, target)
	}
	return SaveFileData{Targets: filtered}
}

func (a *App) scheduleBattleUpload(saveData SaveFileData, filePath, saveName string) {
	if !shouldUploadBattle(saveName) {
		return
	}

	a.mu.RLock()
	playerID := a.selfId
	playerName := a.selfName
	a.mu.RUnlock()

	if playerID == "" {
		logger.Printf("[Upload] 跳过上传：未识别到玩家 ID\n")
		return
	}

	uploadData := filterSaveDataForUpload(saveData)
	if len(uploadData.Targets) == 0 {
		logger.Printf("[Upload] 跳过上传：无符合血量条件的目标\n")
		return
	}

	gzData, err := marshalSaveJSON(uploadData)
	if err != nil {
		logger.Printf("[Upload] 序列化失败: %v\n", err)
		return
	}

	endpoint := strings.TrimSpace(config.UploadEndpoint)
	fileName := filepath.Base(filePath)
	dungeonName := saveName

	go func() {
		if err := postBattleUpload(endpoint, playerID, playerName, dungeonName, fileName, gzData); err != nil {
			logger.Printf("[Upload] 上传失败: %v\n", err)
		}
	}()
}

func postBattleUpload(endpoint, playerID, playerName, dungeonName, fileName string, gzData []byte) error {
	secret := strings.TrimSpace(config.UploadSecret)
	if secret == "" {
		return fmt.Errorf("upload secret not configured")
	}

	nonce, err := newUploadNonce()
	if err != nil {
		return err
	}
	timestamp := time.Now().Unix()
	signature := signBattleUpload(secret, timestamp, nonce, playerID, gzData)

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	_ = writer.WriteField("playerId", playerID)
	if playerName != "" {
		_ = writer.WriteField("playerName", playerName)
	}
	_ = writer.WriteField("dungeonName", dungeonName)
	_ = writer.WriteField("fileName", fileName)
	_ = writer.WriteField("clientVersion", config.ClientVersion)
	_ = writer.WriteField("contentSha256", hashUploadPayload(gzData))

	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return err
	}
	if _, err := io.Copy(part, bytes.NewReader(gzData)); err != nil {
		return err
	}
	if err := writer.Close(); err != nil {
		return err
	}

	req, err := http.NewRequest(http.MethodPost, endpoint, &body)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Authorization", "HMAC-SHA256 "+signature)
	req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
	req.Header.Set("X-Nonce", nonce)

	client := &http.Client{Timeout: battleUploadTimeout}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		logger.Printf("[Upload] 服务端返回非成功状态: %s\n", resp.Status)
	}
	return nil
}
