# Third-party notices

Dieses Projekt bindet die unten aufgeführten Bibliotheken ein. Die Liste deckt die
Abhängigkeiten ab, die tatsächlich in das Binary gelangen, ermittelt mit
`go-licenses csv ./...`. Sie ist bei Dependency-Änderungen zu aktualisieren.

Verteilung: 23 × MIT, 21 × Apache-2.0, 11 × BSD-3-Clause. Keine copyleft-artige
Lizenz.

## Apache-2.0 NOTICE

Abschnitt 4(d) der Apache-2.0 verlangt, die NOTICE-Inhalte eingebundener Werke bei
der Weitergabe mitzuführen.

### github.com/prometheus/common (v0.67.4)

```
Common libraries shared by Prometheus Go components.
Copyright 2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).
```

### github.com/prometheus/procfs (v0.19.2)

```
procfs provides functions to retrieve system, kernel and process
metrics from the pseudo-filesystem proc.

Copyright 2014-2015 The Prometheus Authors

This product includes software developed at
SoundCloud Ltd. (http://soundcloud.com/).
```

### go.yaml.in/yaml/v2 (v2.4.3)

```
Copyright 2011-2016 Canonical Ltd.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0
```

## Vollständige Liste

Erzeugen mit:

```bash
go-licenses csv ./...
```

## Eigener Code

Dieses Projekt steht unter der Apache License 2.0, siehe [LICENSE](LICENSE).
Übernommener Fremdcode ist hier zu dokumentieren — mit Quelle, Lizenz und Datum,
zusätzlich zum Attribution-Header in der betroffenen Datei. Aktuell gibt es keinen.
