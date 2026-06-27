package internal

import (
	"bytes"
	"crypto/md5"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

// UploadResult 上传结果
type UploadResult struct {
	Success bool
	Message string
	Data    interface{}
}

// HttpUploadService HTTP 上传服务
type HttpUploadService struct {
	client *http.Client
}

// NewHttpUploadService 创建 HTTP 上传服务
func NewHttpUploadService() *HttpUploadService {
	return &HttpUploadService{
		client: &http.Client{
			Timeout: 60 * time.Second,
		},
	}
}

// NewHttpUploadServiceWithTimeout 创建带自定义超时时间的上传服务
func NewHttpUploadServiceWithTimeout(timeout time.Duration) *HttpUploadService {
	return &HttpUploadService{
		client: &http.Client{
			Timeout: timeout,
		},
	}
}

// UploadFileToRemoteServer 上传文件到远程服务器
func (s *HttpUploadService) UploadFileToRemoteServer(filePath string, serverURL string) (*UploadResult, error) {
	// 验证文件是否存在
	if !fileExists(filePath) {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("文件不存在: %s", filePath),
		}, nil
	}

	// 解析目标 URL
	targetURL, err := url.Parse(serverURL)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("无效的服务器 URL: %v", err),
		}, nil
	}

	// 读取文件内容
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取文件失败: %v", err),
		}, nil
	}

	fileName := filepath.Base(filePath)

	// 构建 multipart/form-data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件部分
	part, err := writer.CreateFormFile("files", fileName)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建表单文件失败: %v", err),
		}, nil
	}

	if _, err := part.Write(fileContent); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("写入文件内容失败: %v", err),
		}, nil
	}

	if err := writer.Close(); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("关闭表单写入器失败: %v", err),
		}, nil
	}

	// 确定使用 HTTP 还是 HTTPS
	httpClient := s.getHTTPClient(targetURL.Scheme)

	// 发送请求
	req, err := http.NewRequest("POST", serverURL, bytes.NewReader(body.Bytes()))
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建请求失败: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("上传请求失败: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 读取响应内容
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取响应失败: %v", err),
		}, nil
	}

	// 检查状态码
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// 尝试解析响应 JSON
		var responseJSON map[string]interface{}
		if err := json.Unmarshal(responseData, &responseJSON); err == nil {
			return &UploadResult{
				Success: true,
				Message: "上传成功",
				Data:    responseJSON,
			}, nil
		}

		return &UploadResult{
			Success: true,
			Message: "上传成功",
		}, nil
	}

	return &UploadResult{
		Success: false,
		Message: fmt.Sprintf("上传失败: %d %s", resp.StatusCode, string(responseData)),
	}, nil
}

// UploadFileWithFormData 上传文件并附带额外的表单数据
func (s *HttpUploadService) UploadFileWithFormData(filePath string, serverURL string, formData map[string]string) (*UploadResult, error) {
	// 验证文件是否存在
	if !fileExists(filePath) {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("文件不存在: %s", filePath),
		}, nil
	}

	// 解析目标 URL
	targetURL, err := url.Parse(serverURL)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("无效的服务器 URL: %v", err),
		}, nil
	}

	// 读取文件内容
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取文件失败: %v", err),
		}, nil
	}

	fileName := filepath.Base(filePath)

	// 构建 multipart/form-data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件部分
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建表单文件失败: %v", err),
		}, nil
	}

	if _, err := part.Write(fileContent); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("写入文件内容失败: %v", err),
		}, nil
	}

	// 添加其他表单字段
	for key, value := range formData {
		if err := writer.WriteField(key, value); err != nil {
			return &UploadResult{
				Success: false,
				Message: fmt.Sprintf("写入表单字段失败: %v", err),
			}, nil
		}
	}

	if err := writer.Close(); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("关闭表单写入器失败: %v", err),
		}, nil
	}

	// 确定使用 HTTP 还是 HTTPS
	httpClient := s.getHTTPClient(targetURL.Scheme)

	// 发送请求
	req, err := http.NewRequest("POST", serverURL, bytes.NewReader(body.Bytes()))
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建请求失败: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("上传请求失败: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 读取响应内容
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取响应失败: %v", err),
		}, nil
	}

	// 检查状态码
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		// 尝试解析响应 JSON
		var responseJSON map[string]interface{}
		if err := json.Unmarshal(responseData, &responseJSON); err == nil {
			return &UploadResult{
				Success: true,
				Message: "上传成功",
				Data:    responseJSON,
			}, nil
		}

		return &UploadResult{
			Success: true,
			Message: "上传成功",
		}, nil
	}

	return &UploadResult{
		Success: false,
		Message: fmt.Sprintf("上传失败: %d %s", resp.StatusCode, string(responseData)),
	}, nil
}

// ArchiveAndUpload 归档并上传文件
func (s *HttpUploadService) ArchiveAndUpload(filePath string, uploadServerURL string, archiveApplication map[string]interface{}) (*UploadResult, error) {
	// 验证文件是否存在
	if !fileExists(filePath) {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("文件不存在: %s", filePath),
		}, nil
	}

	// 解析目标 URL
	targetURL, err := url.Parse(uploadServerURL)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("无效的服务器 URL: %v", err),
		}, nil
	}

	// 读取文件内容
	fileContent, err := os.ReadFile(filePath)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取文件失败: %v", err),
		}, nil
	}

	fileName := filepath.Base(filePath)

	// 计算文件 MD5
	fileMD5, err := calculateMD5(filePath)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("计算文件 MD5 失败: %v", err),
		}, nil
	}

	// 构建 multipart/form-data
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// 添加文件部分
	part, err := writer.CreateFormFile("file", fileName)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建表单文件失败: %v", err),
		}, nil
	}

	if _, err := part.Write(fileContent); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("写入文件内容失败: %v", err),
		}, nil
	}

	// 添加 MD5 字段
	if err := writer.WriteField("fileMd5", fileMD5); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("写入 MD5 字段失败: %v", err),
		}, nil
	}

	// 添加归档申请信息
	applicationJSON, err := json.Marshal(archiveApplication)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("序列化归档申请失败: %v", err),
		}, nil
	}

	if err := writer.WriteField("archiveApplication", string(applicationJSON)); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("写入归档申请字段失败: %v", err),
		}, nil
	}

	if err := writer.Close(); err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("关闭表单写入器失败: %v", err),
		}, nil
	}

	// 构建归档上传 URL
	archiveURL := &url.URL{
		Scheme: targetURL.Scheme,
		Host:   targetURL.Host,
		Path:   "/api/file/archive",
	}

	// 确定使用 HTTP 还是 HTTPS
	httpClient := s.getHTTPClient(targetURL.Scheme)

	// 发送请求
	req, err := http.NewRequest("POST", archiveURL.String(), bytes.NewReader(body.Bytes()))
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("创建请求失败: %v", err),
		}, nil
	}

	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := httpClient.Do(req)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("归档上传请求失败: %v", err),
		}, nil
	}
	defer resp.Body.Close()

	// 读取响应内容
	responseData, err := io.ReadAll(resp.Body)
	if err != nil {
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("读取响应失败: %v", err),
		}, nil
	}

	// 尝试解析响应 JSON
	var responseJSON map[string]interface{}
	if err := json.Unmarshal(responseData, &responseJSON); err == nil {
		// 检查响应码
		if code, ok := responseJSON["code"].(float64); ok && code == 0 {
			return &UploadResult{
				Success: true,
				Message: "归档成功",
				Data:    responseJSON,
			}, nil
		}
		return &UploadResult{
			Success: false,
			Message: fmt.Sprintf("归档失败: %v", responseJSON["message"]),
			Data:    responseJSON,
		}, nil
	}

	// 检查状态码
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return &UploadResult{
			Success: true,
			Message: "归档成功",
		}, nil
	}

	return &UploadResult{
		Success: false,
		Message: fmt.Sprintf("归档失败: %d %s", resp.StatusCode, string(responseData)),
	}, nil
}

// getHTTPClient 根据协议获取 HTTP 客户端
func (s *HttpUploadService) getHTTPClient(scheme string) *http.Client {
	if scheme == "https" {
		// 对于 HTTPS，使用默认客户端（可以进一步配置 TLS）
		return &http.Client{Timeout: s.client.Timeout}
	}
	return s.client
}

// calculateMD5 计算文件的 MD5 哈希值
func calculateMD5(filePath string) (string, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return "", err
	}

	hash := md5.Sum(data)
	return fmt.Sprintf("%x", hash), nil
}