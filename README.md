Orginally Forked From: https://github.com/SilverMight/vsco-get

# VSCO Media Scraper

A fast, concurrent VSCO media scraper written in Go that downloads all images and videos from VSCO profiles. Features automatic profile picture downloads, progress bars with speed display, and smart duplicate detection.

## ✨ Features

- **Download all media** from any public VSCO profile
- **Automatic profile picture download** - saves alongside media
- **Concurrent downloads** with configurable worker count
- **Progress bars** for each file with download speed
- **Smart duplicate detection** - skips already downloaded files
- **Uses Firefox TLS fingerprint** to bypass bot detection
- **Batch processing** from a list of usernames
- **Files named by ID** - consistent naming from VSCO's API

## 📋 Prerequisites

- Go 1.21 or higher
- Internet connection (obviously)

## 🚀 Installation

### Quick Start

```bash
# Clone the repository
git clone https://github.com/yourusername/vsco-scraper.git
cd vsco-scraper

# Install dependencies
go mod init vsco-scraper
go get github.com/Noooste/azuretls-client
go get github.com/schollz/progressbar/v3

# Run it!
go run scraper.go username
```

### Single File Version

If you prefer a single executable file:

```bash
# Build the binary
go build -o vsco-scraper scraper.go

# Run it
./vsco-scraper username
```

## 📖 Usage

### Single User

```bash
# Download all media from a single user
go run scraper.go username
```

Example:
```bash
go run scraper.go vsco
```

### Multiple Users

Create a text file with usernames (one per line):
```bash
# usernames.txt
vsco
anotheruser
username3
```

Then run:
```bash
go run scraper.go -l usernames.txt
```

### Adjust Worker Count

Control how many files download simultaneously (default: 5):
```bash
# Use 10 workers for faster downloads
go run scraper.go -w 10 username
```

### All Options

```bash
go run scraper.go username           # Single user
go run scraper.go -l list.txt        # Multiple users from file
go run scraper.go -w 10 username      # Custom worker count
go run scraper.go -l list.txt -w 10   # Batch with custom workers
```

## 📁 Output Structure

Files are saved in a `downloads/` directory:

```
downloads/
└── username/
    ├── xxx.jpg  # Image post
    ├── xxx.mp4  # Video post
    └── xxx.jpg  # Profile picture
```

All files are named using their VSCO media ID, making them:
- **Unique** - no filename conflicts
- **Traceable** - easy to find on VSCO
- **Consistent** - same naming for images and videos

## 🎯 How It Works

1. **Gets user info** from `https://vsco.co/api/2.0/sites?subdomain=username`
2. **Extracts site ID** and profile image ID
3. **Downloads profile picture** using the profile image ID
4. **Fetches media list** paginated from `https://vsco.co/api/2.0/medias?site_id=ID&page=N`
5. **Downloads all media** concurrently with progress bars
6. **Skips existing files** - no duplicates

## 🛡️ Anti-Detection

The scraper uses `azuretls-client` with a Firefox browser fingerprint to:
- Spoof TLS/JA3 fingerprints
- Match Firefox HTTP/2 headers
- Avoid 403 Forbidden errors
- Look like a real browser to VSCO

## 📊 Sample Output

```
Found user: vsco (ID: xxx, Profile Image ID: xxx)

Downloading profile picture for vsco...
Profile |████████████████████████████████| 85.6 KB/s | 125 KB/125 KB

Fetched page 1, got 25 items (total: 25)
Found 25 new media items out of 25 total

Downloading 25 media items from vsco with 5 workers...
(Progress bars show individual file downloads)

Image xxx |████████████████████████| 1.2 MB/s | 2.5 MB/2.5 MB
Video xxx |████████████████████████| 3.5 MB/s | 8.1 MB/8.1 MB
Image xxx |████████████████████████| 1.1 MB/s | 1.8 MB/1.8 MB
...

Completed downloads for vsco
```

## 🔧 Troubleshooting

### 401 Unauthorized

If you see `401 Unauthorized`, the authorization token may need updating. VSCO occasionally rotates their tokens. Check the token in the code:

```go
authorizationToken = "Bearer YOUR_TOKEN_HERE"
```

### 403 Forbidden

The Firefox fingerprint should prevent this, but if it occurs:
- Update the `azuretls-client` package: `go get -u github.com/Noooste/azuretls-client`
- Try a different browser fingerprint (Chrome, Safari)

### Rate Limiting

VSCO may rate limit aggressive scraping:
- Reduce worker count with `-w 2`
- Add delays between requests (modify the code if needed)

## 🤝 Contributing

Contributions welcome! Areas for improvement:
- Add video thumbnail downloads
- Support for stories/highlights
- Better error handling
- Configurable output directories
- Resume interrupted downloads

## 📝 License

This project is licensed under the GPL License - see the [LICENSE](LICENSE) file for details.

## ⚠️ Disclaimer

This tool is for educational purposes. Respect VSCO's terms of service and rate limits. Don't use it to mass download content you don't have rights to.

## 🙏 Acknowledgments

- [azuretls-client](https://github.com/Noooste/azuretls-client) for TLS fingerprinting
- [progressbar](https://github.com/schollz/progressbar) for the beautiful progress bars
- The Go community for excellent libraries

---

**Star this repo** if you find it useful! ⭐
