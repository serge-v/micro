# My Plugins

## Commands

| Key         | Command    | Description
|-------------|------------|------------------------------------------------|
|             | goupdate   | Update external golang tools                   |
| Alt+i       | goinstall  | Run go install or go to to the next error      |
| Alt+]       | godef      | Go to the symbol definition                    |
| Alt+t       | godecls    | Show declarations for the current file         |
| Ctrl+Space  | gocomplete | Show go completion list                        |
| Alt+o       | opencur    | Open current directory list in the vsplit      |
| Alt+l       | selectnext | Search next occurence of the word under cursor |
|             | findinfiles| Search word under cursor in ./... files        |
|             | setjumpmode| Go to errors on enter                          |
|             | execcommand| Execute selected text as a shell command       |
|             | textfilter | Runs selected text thru the filter             |

## Abbreviations

Type abbreviation, then space to expand.

| px   | Replacement                               |
|------|-------------------------------------------|
| ie   | if err := _                               |
| ;e   | ;err != nil { _ }                         |
| re   | return err                                |
| lf   | log.Fatal(err)                            |
| tf   | t.Fatal(err)                              |
| ff   | fmt.Printf("_",)                          |
| fp   | fmt.Println(_)                            |
| lp   | log.Println(_)                            |
| ifr  | if err != nil { return err }_             |
| ifl  | if err != nil { log.Fatal(err) }          |
| ifrl | if err := ; err != nil { log.Fatal(err) } |
| ifrr | if err := ; err != nil { return err }     |
