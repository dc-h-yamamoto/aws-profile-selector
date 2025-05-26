# aws-profile-selector
## このリポジトリについて
AWS_DEFAULT_PROFILE 環境変数 をインタラクティブに設定する CLI

全て Gemini 2.5 Pro(Preview) のチャットのみで作成したため、不要なコメント等が残っています。

## build
```shell
go build -o aws-profile-selector
mv aws-profile-selector ~/.local/bin
```


## .bashrc へ下記を追加
```shell
aws-profile-select() {
   local cmd_output
  cmd_output=$(aws-profile-selector)   # aws-profile-selector のパスは適宜変更してください
  local exit_code=$?
  if [ $exit_code -eq 0 ] && [ -n "$cmd_output" ]; then
    echo "$cmd_output"
    eval "$cmd_output"
    echo "AWSプロファイルを $(echo $cmd_output | sed 's/export AWS_DEFAULT_PROFILE=//') に設定しました。"
  fi
}
```

## 実行方法
```shell
aws-profile-select
```

## LICENSE
MIT License