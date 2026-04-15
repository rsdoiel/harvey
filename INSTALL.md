Installation for development of **harvey**
===========================================

**harvey** Harvey is an agent similar to Claude Code but written in Go and designed to use Ollama server to access large language models locally or if configurated access [publicai.co](https://publicai.co) and the Abertus model. It is a terminal based application.

Quick install with curl or irm
------------------------------

There is an experimental installer.sh script that can be run with the following command to install latest table release. This may work for macOS, Linux and if you’re using Windows with the Unix subsystem. This would be run from your shell (e.g. Terminal on macOS).

~~~shell
curl https://Laboratory.github.io/harvey/installer.sh | sh
~~~

This will install the programs included in harvey in your `$HOME/bin` directory.

If you are running Windows 10 or 11 use the Powershell command below.

~~~ps1
irm https://Laboratory.github.io/harvey/installer.ps1 | iex
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

1. git clone https://github.com/Laboratory/harvey
2. Change directory into the `harvey` directory
3. Make to build, test and install

~~~shell
git clone https://github.com/Laboratory/harvey
cd harvey
make
make test
make install
~~~

