package handle

import (
	"fmt"
	"strings"

	"catcher/internal/domain/entity"
	"catcher/internal/dto"
	"catcher/internal/service"
)

// LoginChaoxing 用手机号+密码登录学习通
// 密码只在本次调用内使用，响应只返回登录结果
func (a *App) LoginChaoxing(req dto.PasswordLoginRequest) (res dto.PasswordLoginResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.PasswordLoginResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "学习通登录时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	phone := strings.TrimSpace(req.Phone)
	if phone == "" || req.Password == "" {
		return dto.PasswordLoginResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "请填写手机号和密码"},
		}
	}
	if len(phone) != 11 || strings.Trim(phone, "0123456789") != "" {
		return dto.PasswordLoginResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "学习通账号是 11 位手机号"},
		}
	}

	err := a.svc.LoginChaoxing(a.context(), entity.PasswordCredential{
		Phone:    phone,
		Password: req.Password,
	}, req.Remember)
	if err != nil {
		return dto.PasswordLoginResult{
			OK:          false,
			LoginState:  string(entity.LoginNeedsLogin),
			DisplayName: entity.MaskPhone(phone),
			Error: &dto.APIError{
				Code:    service.ErrorCode(err),
				Message: "学习通登录失败",
				Detail:  err.Error(),
			},
		}
	}
	return dto.PasswordLoginResult{
		OK:          true,
		LoginState:  string(entity.LoginConnected),
		DisplayName: entity.MaskPhone(phone),
	}
}

// ImportTronclassSession 导入浏览器里的学在重邮 session
// 入参与错误详情中只保留脱敏内容
func (a *App) ImportTronclassSession(req dto.SessionImportRequest) (res dto.SessionImportResult) {
	defer func() {
		if r := recover(); r != nil {
			res = dto.SessionImportResult{
				OK:    false,
				Error: &dto.APIError{Code: "INTERNAL", Message: "导入学在重邮会话时发生内部错误", Detail: fmt.Sprint(r)},
			}
		}
	}()

	value := strings.TrimSpace(req.Session)
	if value == "" || strings.EqualFold(value, "session=") {
		return dto.SessionImportResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "请粘贴学在重邮的 session"},
		}
	}
	if len(value) > 4096 {
		return dto.SessionImportResult{
			OK:    false,
			Error: &dto.APIError{Code: "INVALID_ARGUMENT", Message: "session 长度异常"},
		}
	}

	displayName, err := a.svc.ImportTronclassSession(a.context(), value)
	if err != nil {
		return dto.SessionImportResult{
			OK: false,
			Error: &dto.APIError{
				Code:    service.ErrorCode(err),
				Message: "学在重邮会话导入失败",
				Detail:  err.Error(),
			},
		}
	}
	return dto.SessionImportResult{
		OK:          true,
		LoginState:  string(entity.LoginConnected),
		DisplayName: displayName,
	}
}
