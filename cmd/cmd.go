// Package cmd proxyhub 根命令
package cmd

import (
	"go.zoe.im/x/cli"
	"go.zoe.im/x/version"

	"go.zoe.im/proxyhub/internal/config"
)

const description = `proxyhub - 聚合代理池服务

从多源免费代理聚合、健康度评估、智能轮转，兼容 Bright Data / SmartProxy 语义。
一个二进制，零外部依赖，自部署。

  _ __   _ __ ___ __  ___   _| |__  _   _| |__
 | '_ \ | '__/ _ \ \/ / | | | | '_ \| | | | '_ \
 | |_) || | | (_) >  <| |_| | | |_) | |_| | |_) |
 | .__(_)_|  \___/_/\_\\__, |_|_.__/ \__,_|_.__/
 |_|                   |___/
`

var (
	// root command
	cmd = cli.New(
		cli.Name("proxyhub"),
		cli.Short("聚合代理池服务 - HTTP 前向代理 + REST API + Web Dashboard"),
		cli.Description(description),
		cli.GlobalConfig(config.Global, cli.WithConfigName("proxyhub")),
		version.NewOption(true),
		cli.Run(func(c *cli.Command, args ...string) {
			_ = c.Help()
		}),
	)
)

// Register 注册子命令
func Register(scs ...*cli.Command) {
	cmd.Register(scs...)
}

// Run 执行根命令
func Run(opts ...cli.Option) error {
	return cmd.Run(opts...)
}

// Option 追加选项
func Option(opts ...cli.Option) {
	cmd.Option(opts...)
}
