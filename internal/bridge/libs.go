package bridge

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	RepoPaper   = "https://repo.papermc.io/repository/maven-public"
	RepoCentral = "https://repo1.maven.org/maven2"

	// PaperAPIVersion is the paper-api build the bridge is compiled against.
	PaperAPIVersion = "26.3.build.227-stable"
)

// Lib is a pinned Maven artifact. The API jars are not embedded in the kremenmc
// binary; they are downloaded on first start into runtime/libs and verified
// against SHA256. tools/bridgebuild uses the same list to compile the bridge.
type Lib struct {
	Group, Artifact, Version string
	Repo                     string
	SHA256                   string
}

func (l Lib) FileName() string { return l.Artifact + "-" + l.Version + ".jar" }

func (l Lib) URL() string { return l.urlIn(l.Repo) }

func (l Lib) urlIn(repo string) string {
	return repo + "/" + strings.ReplaceAll(l.Group, ".", "/") + "/" + l.Artifact + "/" + l.Version + "/" + l.FileName()
}

// URLs lists download locations in preference order. The PaperMC repository
// proxies Maven Central, so it doubles as a mirror; the pinned hash makes the
// source irrelevant for integrity.
func (l Lib) URLs() []string {
	if l.Repo == RepoPaper {
		return []string{l.URL()}
	}
	return []string{l.URL(), l.urlIn(RepoPaper)}
}

func (l Lib) Coord() string { return l.Group + ":" + l.Artifact + ":" + l.Version }

var Libs = []Lib{
	{"io.papermc.paper", "paper-api", PaperAPIVersion, RepoPaper, "b714f74fef3f9ab378a9a8d4fc5d0f97082d724d0473d775fc809e4934009bed"},
	{"com.mojang", "brigadier", "1.3.11", RepoPaper, "9d09cf4819de83dfcd38bcb212d5f2227131791cef3abbfca3321d6fb2b9a64d"},
	{"net.md-5", "bungeecord-chat", "1.21-R0.2-deprecated+build.21", RepoPaper, "ce930b3ee048520a0ab538eb8d1627b301367fc1b300837235387aded6d89ba1"},
	{"net.kyori", "adventure-api", "5.2.0", RepoCentral, "7e52fe7190be3e87b3b3f71712cfa12315fcd27109f302a7b440c12f01fae827"},
	{"net.kyori", "adventure-key", "5.2.0", RepoCentral, "0184d173200e2eef8fbc791f622d1d58fd459f8930c616b5a4fe79e83eda6c55"},
	{"net.kyori", "adventure-text-serializer-commons", "5.2.0", RepoCentral, "64b3e45d93aa57f4da9b257d16c8f2172612d9e3312b061f4892fd426922d2c3"},
	{"net.kyori", "adventure-text-serializer-json", "5.2.0", RepoCentral, "adb5410006175b9aa6343bfadd0bd11c4a6955ee2abc1a58abe2a3fa6cbd9ab5"},
	{"net.kyori", "adventure-text-serializer-gson", "5.2.0", RepoCentral, "5b2ad1c8a65e7fa0811279b51a45d5fc574b6a6354097b545e4fdb5b68aaed73"},
	{"net.kyori", "adventure-text-serializer-legacy", "5.2.0", RepoCentral, "1231a0de417de66c59e2f2a3892ecca6b8d48621df6f1d9a2f81b01f57e328b7"},
	{"net.kyori", "adventure-text-serializer-plain", "5.2.0", RepoCentral, "f6424cc038a631b79cc4b74b6b353d5d007c99b38f9be482e0c2448a00eecd21"},
	{"net.kyori", "adventure-text-minimessage", "5.2.0", RepoCentral, "e1871aba1511dbe49cff211c10f85682886014161e5d629318aa69583c31a2c0"},
	{"net.kyori", "adventure-text-logger-slf4j", "5.2.0", RepoCentral, "ae79b7f3846c5d973b37c7eec03190bd44e182918d1346e7705c5991a1b5dfb3"},
	{"net.kyori", "option", "1.1.0", RepoCentral, "97b69b4b17dfe02217c9131ad342564cbc9aebd04c75eb689639b5f78fd4b11c"},
	{"com.google.guava", "guava", "33.6.0-jre", RepoCentral, "dc573e1fca4fd5454f4a5fd3d7da2df03002876a4175bafc14a95980dd7713b3"},
	{"com.google.guava", "failureaccess", "1.0.3", RepoCentral, "cbfc3906b19b8f55dd7cfd6dfe0aa4532e834250d7f080bd8d211a3e246b59cb"},
	{"com.google.code.gson", "gson", "2.14.0", RepoCentral, "2cbd119bf1961c28788310963dc80ba65f58cdeec1dd139c8bdb1240faa2c36f"},
	{"org.yaml", "snakeyaml", "2.2", RepoCentral, "1467931448a0817696ae2805b7b8b20bfb082652bf9c4efaed528930dc49389b"},
	{"org.joml", "joml", "1.10.9", RepoCentral, "feca4db853371704338621c120acc5cc32300d8af635fa42414b6213301e216b"},
	{"it.unimi.dsi", "fastutil", "8.5.18", RepoCentral, "9094ae67d01d0ad246f886f11ad557fc2e79c72cbf3feed83e1512a8ae90a74a"},
	{"org.apache.logging.log4j", "log4j-api", "2.26.0", RepoCentral, "820de7bbaf430eaf2e3e5ccb92f23e2085797c5ac4d4f7fc6c8b660fbefa1dbc"},
	// Bundled by Paper; plugins install log filters on its loggers.
	{"org.apache.logging.log4j", "log4j-core", "2.26.0", RepoCentral, "406bb3132c47a5d322e5c571feb0787ca84496469f346eb51f1ef2ee45499902"},
	{"org.slf4j", "slf4j-api", "2.0.17", RepoCentral, "7b751d952061954d5abfed7181c1f645d336091b679891591d63329c622eb832"},
	{"org.jspecify", "jspecify", "1.0.0", RepoCentral, "1fad6e6be7557781e4d33729d49ae1cdc8fdda6fe477bb0cc68ce351eafdfbab"},
	{"org.checkerframework", "checker-qual", "4.2.3", RepoCentral, "f868f731f6e37db3e1c2140d06016477989959a2d13fc5bed9c46760cffd5ba0"},
	{"org.jetbrains", "annotations", "26.1.0", RepoCentral, "ebc7aec252ed0c7d2d04c039d7f00e69f7b86b1f493c741d67b3ef31b986b054"},
	// Not an API dependency, but bundled by Paper; SimpleCommandMap and many plugins use it.
	{"org.apache.commons", "commons-lang3", "3.17.0", RepoCentral, "6ee731df5c8e5a2976a1ca023b6bb320ea8d3539fbe64c8a1d5cb765127c33b4"},
	// Bundled by Paper; the NMS compatibility layer gives each player a Netty channel.
	{"io.netty", "netty-common", "4.2.18.Final", RepoCentral, "5d97cae5669685872339698efe13f74fe3cdb2dccdb36963b2352bd95acf7070"},
	{"io.netty", "netty-buffer", "4.2.18.Final", RepoCentral, "fdf236d2b76aa9710684401fdad7dff9dec56e43da5d39172c9a55ba5f14b360"},
	{"io.netty", "netty-resolver", "4.2.18.Final", RepoCentral, "68373ec544cf769ba17bf2ef455166d98f2c260dff536edf35ef57067a9c155f"},
	{"io.netty", "netty-transport", "4.2.18.Final", RepoCentral, "eac4f12068db4489e60c6520fad663e5d872f0436a7c641aa9e08db649947863"},
	{"io.netty", "netty-codec-base", "4.2.18.Final", RepoCentral, "7e4612bead7ba88ac6f7908fa722f90ad6835ea96505c9e40b2f93686c1f61f3"},
}

const maxLibSize = 64 << 20

// EnsureLibs makes sure every lib is present in dir with the pinned hash,
// downloading missing or corrupt ones.
func EnsureLibs(ctx context.Context, client *http.Client, dir string, libs []Lib, log *slog.Logger) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("libs dir: %w", err)
	}
	for _, l := range libs {
		path := filepath.Join(dir, l.FileName())
		if ok, _ := FileHasSHA256(path, l.SHA256); ok {
			continue
		}
		if log != nil {
			log.Info("downloading bridge library", "lib", l.Coord())
		}
		var err error
		for _, u := range l.URLs() {
			if err = DownloadVerified(ctx, client, u, path, l.SHA256, maxLibSize); err == nil {
				break
			}
		}
		if err != nil {
			return fmt.Errorf("lib %s: %w", l.Coord(), err)
		}
	}
	return nil
}

func FileHasSHA256(path, want string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return false, err
	}
	return want != "" && hex.EncodeToString(h.Sum(nil)) == strings.ToLower(want), nil
}

// DownloadVerified downloads url to dest atomically, failing unless the
// content hashes to wantSHA256. An empty wantSHA256 is rejected.
func DownloadVerified(ctx context.Context, client *http.Client, url, dest, wantSHA256 string, limit int64) error {
	if wantSHA256 == "" {
		return fmt.Errorf("no pinned sha256 for %s", url)
	}
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("download: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("download %s: %s", url, resp.Status)
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), ".dl-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	h := sha256.New()
	n, err := io.Copy(io.MultiWriter(tmp, h), io.LimitReader(resp.Body, limit+1))
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		return fmt.Errorf("download %s: %w", url, err)
	}
	if n > limit {
		return fmt.Errorf("download %s: larger than %d bytes", url, limit)
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != strings.ToLower(wantSHA256) {
		return fmt.Errorf("download %s: sha256 mismatch: got %s want %s", url, got, wantSHA256)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		return fmt.Errorf("install %s: %w", dest, err)
	}
	return nil
}
