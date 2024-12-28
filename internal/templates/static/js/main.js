class VirtualFontList {
    constructor(container, options = {}) {
        this.container = container;
        this.fonts = [];
        this.loadedFonts = new Map();
        this.visibleItems = new Set();
        this.options = {
            itemHeight: options.itemHeight || 300,
            defaultFontSize: options.defaultFontSize || 24
        };
        // Set initial CSS variable
        document.documentElement.style.setProperty('--preview-font-size', `${this.options.defaultFontSize}px`);
    }

    // Modify init method to reset button
    init(fonts) {
        this.fonts = [...fonts];
        this.renderItems();
        this.loadAllFonts();
        
        // Set up download all functionality
        const downloadContainer = document.getElementById('downloadAllFonts');
        const downloadBtn = downloadContainer.querySelector('button');
        downloadBtn.addEventListener('click', this.downloadAllFonts.bind(this));
        
        // Reset button state
        this.resetDownloadButton();
    }

    renderItems() {
        this.container.innerHTML = '';
        const fragment = document.createDocumentFragment();
        
        this.fonts.forEach((font, index) => {
            const div = document.createElement('div');
            div.className = 'font-item';
            div.dataset.index = index;
            div.dataset.fontName = font.name.toLowerCase();
            div.innerHTML = this.getPlaceholderContent(font);
            fragment.appendChild(div);
        });
        
        this.container.appendChild(fragment);
    }

    getPlaceholderContent(font) {
        return `
            <div class="font-header">
                <h3>${font.name}</h3>
                <div class="font-actions">
                    ${this.generateFormatButtons(font.formats)}
                </div>
            </div>
            <div class="font-preview">
                <div>Loading preview...</div>
            </div>`;
    }

    async loadAllFonts() {
        const loadPromises = this.fonts.map((font, index) => this.loadFontItem(index));
        await Promise.all(loadPromises);
    }

    async loadFontItem(index) {
        const element = this.container.children[index];
        const font = this.fonts[index];
        
        if (!element || !font || this.visibleItems.has(index)) return;

        if (this.loadedFonts.has(font.name)) {
            element.innerHTML = this.loadedFonts.get(font.name);
            this.visibleItems.add(index);
            return;
        }

        const content = `
            <div class="font-header">
                <h3>${font.name}</h3>
                <div class="font-actions">
                    ${this.generateFormatButtons(font.formats)}
                </div>
            </div>
            <div class="font-preview">
                <style>
                    @font-face {
                        font-family: "${font.name}";
                        src: url("${font.preview}") format("woff2");
                        font-display: swap;
                    }
                </style>
                <div style="font-family: '${font.name}';" class="preview-text">
                    ${document.getElementById('sampleText').value}
                </div>
            </div>`;
        
        this.loadedFonts.set(font.name, content);
        element.innerHTML = content;
        this.visibleItems.add(index);
    }

    generateFormatButtons(formats) {
        return Object.entries(formats)
            .map(([format, url]) => `
                <a href="${url}" download class="format-button">
                    ${format.toUpperCase().replace('.', '')}
                </a>
            `).join('');
    }

    sort(order) {
        this.fonts.sort((a, b) => {
            if (order === 'asc') {
                return a.name.localeCompare(b.name);
            } else {
                return b.name.localeCompare(a.name);
            }
        });
        
        this.visibleItems.clear();
        this.renderItems();
        this.loadAllFonts();
        this.updateDownloadAllButton();
    }

    filter(searchTerm) {
        this.currentFilter = searchTerm.toLowerCase();
        let visibleCount = 0;
        
        Array.from(this.container.children).forEach(element => {
            const fontName = element.dataset.fontName;
            const matches = fontName.includes(this.currentFilter);
            element.style.display = matches ? '' : 'none';
            if (matches) visibleCount++;
        });
    
        const fontCountMessage = document.getElementById('fontCountMessage');
        if (visibleCount === 0) {
            fontCountMessage.textContent = 'No fonts found';
        } else if (visibleCount === 1) {
            fontCountMessage.textContent = '1 font found';
        } else {
            fontCountMessage.textContent = `${visibleCount} fonts found`;
        }

        // Update download all button after filtering
        this.updateDownloadAllButton();
    }

    updateDownloadAllButton() {
        const downloadContainer = document.getElementById('downloadAllFonts');
        const downloadBtn = downloadContainer.querySelector('button');
        
        // Get currently visible fonts after filtering
        const visibleFonts = Array.from(this.container.children)
            .filter(el => el.style.display !== 'none');
        
        // Update button text and visibility
        if (visibleFonts.length > 0) {
            downloadContainer.style.display = 'block';
            downloadBtn.textContent = visibleFonts.length === 1 
                ? 'Download 1 Font (.zip)' 
                : `Download All ${visibleFonts.length} Fonts (.zip)`;
        } else {
            downloadContainer.style.display = 'none';
        }
    }

    downloadAllFonts() {
        // Disable the button immediately to prevent multiple clicks
        const downloadBtn = document.getElementById('downloadAllFonts').querySelector('button');
        downloadBtn.disabled = true;
        downloadBtn.classList.add('disabled');
    
        if (!this.fonts || this.fonts.length === 0) {
            alert('No fonts to download.');
            this.resetDownloadButton();
            return;
        }
    
        // Get currently visible fonts to download
        const visibleFontIndices = Array.from(this.container.children)
            .filter(el => el.style.display !== 'none')
            .map(el => parseInt(el.dataset.index));
    
        // Filter fonts based on visible items
        const fontsToDownload = visibleFontIndices.map(index => this.fonts[index]);
    
        // Show loading state
        const originalText = downloadBtn.textContent;
        downloadBtn.textContent = 'Preparing Download...';
    
        // Prepare the font data with standardized paths
        const fontData = {
            fonts: fontsToDownload.map(font => ({
                name: font.name,
                formats: Object.fromEntries(
                    Object.entries(font.formats).map(([ext, originalPath]) => [
                        ext, 
                        originalPath.startsWith('/download?path=') 
                            ? originalPath 
                            : `/download?path=${encodeURIComponent(originalPath)}`
                    ])
                )
            }))
        };
    
        // Send request to create zip file
        fetch('/download-all', {
            method: 'POST',
            headers: {
                'Content-Type': 'application/json',
            },
            body: JSON.stringify(fontData)
        })
        .then(response => {
            if (!response.ok) {
                // Try to get error message from response
                return response.text().then(text => {
                    console.error('Download error response:', text);
                    throw new Error(text || 'Download failed');
                });
            }
            return response.blob();
        })
        .then(blob => {
            // Create download link
            const url = window.URL.createObjectURL(blob);
            const a = document.createElement('a');
            a.href = url;
            a.download = `fonts-${new Date().toISOString().slice(0,10)}.zip`;
            document.body.appendChild(a);
            a.click();
            window.URL.revokeObjectURL(url);
            document.body.removeChild(a);
    
            // Show download complete message and keep button disabled
            downloadBtn.textContent = 'Zipped Fonts Downloaded';
            downloadBtn.disabled = true;
            downloadBtn.classList.add('disabled');
        })
        .catch(error => {
            console.error('Complete download error:', error);
            alert(`Failed to download fonts: ${error.message}`);
            this.resetDownloadButton();
        });
    }
    
    resetDownloadButton() {
        const downloadBtn = document.getElementById('downloadAllFonts').querySelector('button');
        downloadBtn.disabled = false;
        downloadBtn.classList.remove('disabled');
        this.updateDownloadAllButton();
    }

    updateFontSize(size) {
        this.options.defaultFontSize = size;
        // Update CSS variable instead of individual elements
        document.documentElement.style.setProperty('--preview-font-size', `${size}px`);
    }

    destroy() {
        this.container.innerHTML = '';
        this.visibleItems.clear();
        this.loadedFonts.clear();
    }
}

// Theme toggle
function toggleTheme() {
    document.body.classList.toggle('dark-theme');
    const isDark = document.body.classList.contains('dark-theme');
    localStorage.setItem('theme', isDark ? 'dark' : 'light');
}

// Initialize theme from localStorage
if (localStorage.getItem('theme') === 'dark') {
    document.body.classList.add('dark-theme');
}

// Progress handling
function initializeProgress() {
    const loading = document.getElementById('loading');
    const loadingDetails = document.getElementById('loadingDetails');
    const progressBar = document.getElementById('progressBar');
    
    loading.style.display = 'block';
    progressBar.style.width = '0%';

    const eventSource = new EventSource('/progress');
    
    eventSource.onmessage = function(event) {
        loadingDetails.textContent = event.data;
        
        const match = event.data.match(/(\d+)\/(\d+)/);
        if (match) {
            const [current, total] = match.slice(1).map(Number);
            const percentage = (current / total) * 100;
            progressBar.style.width = `${percentage}%`;
        }
    };

    eventSource.onerror = function() {
        eventSource.close();
        loading.style.display = 'none';
    };

    return eventSource;
}

// Global instance
let virtualFontList;

// Document ready handler
document.addEventListener('DOMContentLoaded', function() {
    // Form submit handler
    document.getElementById('previewForm').addEventListener('submit', async function(e) {
        e.preventDefault();
        
        if (virtualFontList) {
            virtualFontList.destroy();
        }

        document.getElementById('filterInput').value = '';
        const results = document.getElementById('results');
        const message = document.getElementById('message');
        results.innerHTML = '';
        message.innerHTML = '';
        document.getElementById('totalFonts').style.display = 'none';

        try {
            const fontDir = document.getElementById('fontDir').value;
            const eventSource = initializeProgress();
            const response = await fetch(`/generate?fontDir=${encodeURIComponent(fontDir)}`);
            const data = await response.json();
            
            eventSource.close();
            document.getElementById('loading').style.display = 'none';

            if (data.error) {
                message.innerHTML = `<div class="error-message">${data.error}</div>`;
                return;
            }

            virtualFontList = new VirtualFontList(results, {
                itemHeight: 300,
                defaultFontSize: parseInt(document.getElementById('fontSize').value)
            });
            
            virtualFontList.init(data);

            const fontCountMessage = document.getElementById('fontCountMessage');
            if (data.length === 0) {
                fontCountMessage.textContent = 'No fonts found';
            } else if (data.length === 1) {
                fontCountMessage.textContent = '1 font found';
            } else {
                fontCountMessage.textContent = `${data.length} fonts found`;
            }
            document.getElementById('totalFonts').style.display = 'block';
            
        } catch (error) {
            document.getElementById('loading').style.display = 'none';
            message.innerHTML = `<div class="error-message">Error processing request: ${error.message}</div>`;
        }
    });

    document.getElementById('fontSize').addEventListener('change', function(e) {
        if (virtualFontList) {
            virtualFontList.updateFontSize(parseInt(e.target.value));
        }
    });

    // Column selector
    document.getElementById('columns').addEventListener('change', function(e) {
        const results = document.getElementById('results');
        results.className = `grid grid-${e.target.value}`;
    });

    // Sort order
    document.getElementById('sortOrder').addEventListener('change', function(e) {
        if (virtualFontList) {
            virtualFontList.sort(e.target.value);
        }
    });

    // Font filter
    document.getElementById('filterInput').addEventListener('input', function(e) {
        if (virtualFontList) {
            virtualFontList.filter(e.target.value);
            virtualFontList.resetDownloadButton();
        }
    });

    // Sample text changes
    document.getElementById('sampleText').addEventListener('input', function(e) {
        if (virtualFontList) {
            const fonts = virtualFontList.fonts;
            const currentFilter = document.getElementById('filterInput').value;
            
            virtualFontList.destroy();
            virtualFontList = new VirtualFontList(document.getElementById('results'), {
                itemHeight: 300,
                defaultFontSize: parseInt(document.getElementById('fontSize').value)
            });
            virtualFontList.init(fonts);
            
            // Reapply filter if it exists
            if (currentFilter) {
                document.getElementById('filterInput').value = currentFilter;
                virtualFontList.filter(currentFilter);
            }
        }
    });
});