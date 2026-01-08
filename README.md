# Rainstorm Peer

Rainstorm Peer is the client-side application for the Rainstorm file sharing system. It enables users to share and download files in a peer-to-peer network using QUIC for high-performance data transfer and a centralized tracker for peer discovery.

## Features

- **P2P File Transfer**: Direct file transfer between peers using QUIC.
- **Tracker Integration**: Communicates with a tracker to register files and discover peers.
- **Chunked File Handling**: Breaks large files into chunks for efficient storage and transfer.
- **Resume Capability**: Supports saving and loading the application state, allowing transfers to resume (logic present, requires specialized usage).
- **Parallel Downloading**: Downloads chunks from multiple peers simultaneously.

## Prerequisites

- [Go](https://go.dev/doc/install) 1.22.5 or later.
- Access to a running Rainstorm Tracker instance.

## Installation

1.  Clone the repository:
    ```bash
    git clone https://github.com/aadit-n3rdy/rainstorm_peer.git
    cd rainstorm_peer
    ```

2.  Build the application:
    ```bash
    go build -o peer
    ```

## Usage

Run the peer application:

```bash
./peer
```

The application provides an interactive command-line interface. The following commands are available:

### Commands

-   **`push`**: Share a local file with the network.
    -   Prompts for:
        -   **Local file name**: Path to the file on your disk.
        -   **File ID**: A unique identifier for the file in the network.
        -   **File name**: The name of the file as it should appear to other peers.
        -   **Tracker IP**: The IP address of the tracker.

-   **`pull`**: Download a file from the network.
    -   Prompts for:
        -   **Local file name**: Where to save the downloaded file.
        -   **File ID**: The unique identifier of the file to download.
        -   **Tracker IP**: The IP address of the tracker.

-   **`save`**: Save the current state (tracked files, chunks) to disk.
    -   Saves status to the configured `RSTM_SAVE_PATH`.

-   **`load`**: Load a previously saved state from disk.
    -   Restores file tracking and chunk information.

-   **`exit`**: Save the current state and exit the application.

### Configuration

You can configure the storage location for application data using the `RSTM_SAVE_PATH` environment variable.

-   **`RSTM_SAVE_PATH`**: Directory where chunks and state files are stored. Defaults to `./rstm_save` in the current working directory if not set.

```bash
export RSTM_SAVE_PATH=/path/to/my/storage
./peer
```

## Certificates

The application uses TLS for secure QUIC connections. Ensure you have `cert.pem` and `key.pem` in the working directory or update the `generateTLSConfig` function in `peer.go` to point to your certificate files.
