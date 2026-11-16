# Template of the formula that goreleaser generates into dist/homebrew/Formula/ (.goreleaser.yaml, `brews`).
# The release pipeline fills the __PLACEHOLDERS__ (sha256 values from checksums.txt); the version is bumped with the release (P5-05).
class Sdkit < Formula
  desc "Spec-driven development workflow and guards for AI coding agents (Claude Code, Codex, OpenCode, Cursor, Gemini)"
  homepage "https://github.com/wecii/sdkit"
  version "2.1.1"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_arm64.tar.gz"
      sha256 "35ed9049739cf52097cb7e61378fe43526ac33237e6dc64d7834c98f351bc693"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_darwin_amd64.tar.gz"
      sha256 "87a457a7622f57ecf3a4787cc8d0901e3d2c555332c7b0a3238cc721fb8f14c3"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_arm64.tar.gz"
      sha256 "ab0b6b2ad89725a0dea678548b9b08bd99f8a87bb872a41408f278fe6946d691"
    else
      url "https://github.com/wecii/sdkit/releases/download/v#{version}/sdkit_#{version}_linux_amd64.tar.gz"
      sha256 "3df2662685c1344dbfaa2ca973db61af7eb61fbb6e67f3ed62ed6d2b75a3af3d"
    end
  end

  def install
    bin.install "sdkit"
  end

  def caveats
    <<~EOS
      Set a repo up (once per repo, by one person):
        cd your-repo && sdkit init
      Check a repo any time:  sdkit doctor
      Upgrade a repo after "brew upgrade":  sdkit upgrade
    EOS
  end

  test do
    assert_match "sdkit", shell_output("#{bin}/sdkit version")
    refute_empty shell_output("#{bin}/sdkit where").strip
  end
end
