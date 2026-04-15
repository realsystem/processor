// Processor Control Plane - Main JavaScript

document.addEventListener('DOMContentLoaded', function() {
    // Flash message dismissal
    const flashCloseButtons = document.querySelectorAll('.flash-close');
    flashCloseButtons.forEach(function(button) {
        button.addEventListener('click', function() {
            const flash = this.closest('.flash');
            if (flash) {
                flash.style.opacity = '0';
                setTimeout(function() {
                    flash.remove();
                }, 200);
            }
        });
    });

    // Copy to clipboard functionality
    const copyButtons = document.querySelectorAll('[data-copy]');
    copyButtons.forEach(function(button) {
        button.addEventListener('click', function() {
            const text = this.getAttribute('data-copy');
            if (navigator.clipboard) {
                navigator.clipboard.writeText(text).then(function() {
                    const originalText = button.textContent;
                    button.textContent = 'Copied!';
                    setTimeout(function() {
                        button.textContent = originalText;
                    }, 2000);
                }).catch(function(err) {
                    console.error('Failed to copy:', err);
                });
            }
        });
    });

    // Confirm delete/revoke actions
    const confirmButtons = document.querySelectorAll('[data-confirm]');
    confirmButtons.forEach(function(button) {
        button.addEventListener('click', function(e) {
            const message = this.getAttribute('data-confirm');
            if (!confirm(message)) {
                e.preventDefault();
                return false;
            }
        });
    });

    // Toggle expandable sections
    const toggleButtons = document.querySelectorAll('[data-toggle]');
    toggleButtons.forEach(function(button) {
        button.addEventListener('click', function() {
            const targetId = this.getAttribute('data-toggle');
            const target = document.getElementById(targetId);
            if (target) {
                target.style.display = target.style.display === 'none' ? 'block' : 'none';
            }
        });
    });

    // Auto-dismiss flash messages after 5 seconds
    const flashes = document.querySelectorAll('.flash:not(.flash-error)');
    flashes.forEach(function(flash) {
        setTimeout(function() {
            flash.style.opacity = '0';
            setTimeout(function() {
                flash.remove();
            }, 200);
        }, 5000);
    });
});
