Installation for development of **harvey**
===========================================

**harvey** Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally. It is a terminal based application.

The Harvey name was inspired by the play of that name by Mary Chase. I saw parallels between the story Harvey and what I see as my personal language model agent. Most people think only of the over hyped big companies wasting billions. I think of my little computers and what they can accomplish. Harvey in the play is a mythic creature. Harvey is a Púca, my Harvey is similar in this commercial AI hype craze. How I did learned about Harvey? When I was young I saw the film on television called [Harvey](https://en.wikipedia.org/wiki/Harvey_(1950_film)) featuring James Stewart. I remember really liking the film as much as I like another old film called Topper. Today I like the idea of a software Harvey that those who take time to see it, or in the case run it on a little computer, can have an adventure and some fun with it.

Quick install with curl or irm
------------------------------

There is an experimental installer.sh script that can be run with the following command to install latest table release. This may work for macOS, Linux and if you’re using Windows with the Unix subsystem. This would be run from your shell (e.g. Terminal on macOS).

~~~shell
curl https://rsdoiel.github.io/harvey/installer.sh | sh
~~~

This will install the programs included in harvey in your `$HOME/bin` directory.

If you are running Windows 10 or 11 use the Powershell command below.

~~~ps1
irm https://rsdoiel.github.io/harvey/installer.ps1 | iex
~~~

### If your are running macOS or Windows

You may get security warnings if you are using macOS or Windows. See the notes for the specific operating system you're using to fix issues.

- [INSTALL_NOTES_macOS.md](INSTALL_NOTES_macOS.md)
- [INSTALL_NOTES_Windows.md](INSTALL_NOTES_Windows.md)

Installing from source
----------------------

### Required software

- Go &gt;&#x3D; 1.26.2

### Steps

1. git clone https://github.com/rsdoiel/harvey
2. Change directory into the `harvey` directory
3. Make to build, test and install

~~~shell
git clone https://github.com/rsdoiel/harvey
cd harvey
make
make test
make install
~~~

