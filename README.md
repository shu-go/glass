make overwrapping windows be transparent

[![Go Report Card](https://goreportcard.com/badge/github.com/shu-go/glass)](https://goreportcard.com/report/github.com/shu-go/glass)
![MIT License](https://img.shields.io/badge/License-MIT-blue)

# Usage

## Sub commands

```
Sub commands:
  watch, w  run constantly
  list, ls  list overwrapping windows
  temp      run once
  recover   force all windows untransparent

Options:
  -v, --verbose  verbose output to stderr
```

```
glass watch [window_title | process_name]
```

## An example

```
glass watch メモ
```

or

```
glass watch notepad
```

![Screenshot](https://raw.githubusercontent.com/shu-go/glass/assets/glass_1.png)

The windows other than Notepad are transparent so that you can see the contents of Notepad.

You can do your work while playing the video behind. :smiling_imp:

<!-- vim: set et ft=markdown sts=4 sw=4 ts=4 tw=0 : -->
