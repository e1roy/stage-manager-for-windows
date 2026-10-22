using System.Windows;
using System.Windows.Input;
using System.Windows.Media;
using StageManager.Controls;
using StageManager.Interop;
using StageManager.Models;
using StageManager.Services;
using MouseEventArgs = System.Windows.Input.MouseEventArgs;
using RectangleF = System.Drawing.RectangleF;

namespace StageManager;

public partial class MainWindow
{
    private readonly List<WindowPreviewControl> _cards = [];
    private readonly List<WindowPreviewControl> _removingCards = [];
    private const double StripPadding = 24;

    private readonly Spring _scroll = new(0, response: 0.36);
    private readonly Spring _stripScale = new(1, response: 0.4);
    private CardLayout _cardLayout = LayoutMetrics.Calculate(StageManagerSettings.Default.DisplayWidth);
    private double _cardLayoutWidth = double.NaN;
    private int _scrollIndex;
    private int _visibleSlots = 1;
    private double _itemExtent = 158;
    private double _slotExtent = 158;
    private double _viewportTop;

    private IEnumerable<WindowPreviewControl> AllCards => _cards.Concat(_removingCards);

    private double StripHeight => double.IsNaN(Height) ? ActualHeight : Height;

    /// <summary>
    /// Reconciles the strip with <paramref name="groups"/>. Existing cards keep
    /// their springs so they glide to new slots; new cards grow in place and
    /// removed cards shrink away. Cards listed in <paramref name="inFlight"/>
    /// start hidden because their windows are still flying in.
    /// </summary>
    private void SyncStrip(IReadOnlyList<AppGroup> groups, IReadOnlySet<string>? inFlight = null)
    {
        var width = double.IsNaN(Width) ? ActualWidth : Width;
        if (width != _cardLayoutWidth)
        {
            // Card geometry follows the panel width; rebuild instead of morphing.
            ClearCards();
            _cardLayoutWidth = width;
        }

        var existing = _cards.ToDictionary(static card => card.Key, StringComparer.Ordinal);
        var next = new List<WindowPreviewControl>(groups.Count);
        foreach (var group in groups)
        {
            var icon = _windowIconService.GetIcon(group.Front);
            if (existing.Remove(group.Key, out var card))
            {
                card.Update(group, icon);
            }
            else if (_removingCards.FirstOrDefault(candidate => candidate.Key == group.Key && !candidate.IsInFlight) is { } leaving)
            {
                // The app came back while its card was still fading out.
                _removingCards.Remove(leaving);
                leaving.IsRemoving = false;
                leaving.Presence.Target = 1;
                leaving.Update(group, icon);
                card = leaving;
            }
            else
            {
                card = CreateCard(group, icon, inFlight?.Contains(group.Key) == true);
            }

            next.Add(card);
        }

        foreach (var card in existing.Values)
        {
            card.IsRemoving = true;
            card.Presence.Target = 0;
            card.Hover.Target = 0;
            _removingCards.Add(card);
        }

        _cards.Clear();
        _cards.AddRange(next);
        EmptyState.Visibility = _cards.Count == 0 && _settings.DisplayMode is DisplayMode.Fixed
            ? Visibility.Visible
            : Visibility.Collapsed;
        LayoutStrip();
    }

    private WindowPreviewControl CreateCard(AppGroup group, ImageSource? icon, bool isInFlight)
    {
        var card = new WindowPreviewControl(group, icon, _cardLayout, _cardLayoutWidth)
        {
            IsInFlight = isInFlight
        };
        if (isInFlight)
        {
            card.Presence.Snap(1);
        }
        else
        {
            card.Presence.Target = 1;
        }

        card.MouseEnter += Card_MouseEnter;
        card.MouseLeave += Card_MouseLeave;
        card.MouseLeftButtonDown += Card_MouseLeftButtonDown;
        card.MouseMove += Card_MouseMove;
        card.MouseLeftButtonUp += Card_MouseLeftButtonUp;
        card.LostMouseCapture += Card_LostMouseCapture;
        card.ThumbnailsReplaced += (_, _) => RequestFrame();
        StripCanvas.Children.Add(card);
        if (_windowHandle != 0)
        {
            card.AttachThumbnails(_windowHandle);
        }

        return card;
    }

    private void ClearCards()
    {
        _cardLayoutWidth = double.NaN;
        foreach (var card in AllCards)
        {
            card.ReleaseThumbnails();
            _iconOverlay?.RemoveBadge(card.Key);
        }

        _cards.Clear();
        _removingCards.Clear();
        StripCanvas.Children.Clear();
    }

    /// <summary>
    /// Assigns each card its slot; springs carry them there on the next frames.
    /// Like macOS, cards shrink so every app fits the screen height, and the
    /// strip only scrolls once they reach their smallest size.
    /// </summary>
    private void LayoutStrip()
    {
        var count = Math.Max(1, _cards.Count);
        var available = Math.Max(_itemExtent * LayoutMetrics.MinimumCardScale, StripHeight - 2 * StripPadding);
        var scale = Math.Clamp(available / (count * _itemExtent), LayoutMetrics.MinimumCardScale, 1);
        _stripScale.Target = scale;
        _slotExtent = _itemExtent * scale;
        _visibleSlots = Math.Clamp((int)Math.Floor(available / _slotExtent + 0.001), 1, count);
        _viewportTop = (StripHeight - _visibleSlots * _slotExtent) / 2;
        _scrollIndex = Math.Clamp(_scrollIndex, 0, count - _visibleSlots);
        _scroll.Target = _scrollIndex;
        for (var index = 0; index < _cards.Count; index++)
        {
            var card = _cards[index];
            card.CenterY.Target = _viewportTop + (index + 0.5) * _slotExtent;
            if (card.IsNew)
            {
                card.CenterY.Snap(card.CenterY.Target);
                card.IsNew = false;
            }
        }

        RequestFrame();
    }

    private void StripRoot_MouseWheel(object sender, MouseWheelEventArgs e)
    {
        e.Handled = true;
        if (_isTransitioning || _cards.Count <= _visibleSlots)
        {
            return;
        }

        var next = Math.Clamp(_scrollIndex + (e.Delta < 0 ? 1 : -1), 0, _cards.Count - _visibleSlots);
        if (next != _scrollIndex)
        {
            _scrollIndex = next;
            LayoutStrip();
        }
    }

    private void Card_MouseEnter(object sender, MouseEventArgs e)
    {
        if (sender is WindowPreviewControl { IsRemoving: false } card)
        {
            card.Hover.Target = 1;
            RequestFrame();
        }
    }

    private void Card_MouseLeave(object sender, MouseEventArgs e)
    {
        if (sender is WindowPreviewControl card)
        {
            card.Hover.Target = 0;
            RequestFrame();
        }
    }

    private void RequestFrame()
    {
        if (_isRenderingSubscribed)
        {
            return;
        }

        _lastFrameTime = _frameClock.Elapsed.TotalSeconds;
        CompositionTarget.Rendering += CompositionTarget_Rendering;
        _isRenderingSubscribed = true;
    }

    private void UnsubscribeRendering()
    {
        if (!_isRenderingSubscribed)
        {
            return;
        }

        CompositionTarget.Rendering -= CompositionTarget_Rendering;
        _isRenderingSubscribed = false;
    }

    private void CompositionTarget_Rendering(object? sender, EventArgs e)
    {
        var now = _frameClock.Elapsed.TotalSeconds;
        var elapsed = Math.Clamp(now - _lastFrameTime, 0, 0.05);
        _lastFrameTime = now;

        var isAnimating = _panelReveal.Step(elapsed);
        isAnimating |= _scroll.Step(elapsed);
        isAnimating |= _stripScale.Step(elapsed);
        foreach (var card in AllCards)
        {
            isAnimating |= card.Step(elapsed);
        }

        foreach (var card in _removingCards.Where(static card => card.Presence.IsSettled).ToArray())
        {
            card.ReleaseThumbnails();
            _iconOverlay?.RemoveBadge(card.Key);
            StripCanvas.Children.Remove(card);
            _removingCards.Remove(card);
        }

        ApplyStripVisuals();
        if (_panelReveal.Target == 0 && _panelReveal.IsSettled && IsVisible)
        {
            ConcealWindow();
        }

        if (!isAnimating && _drag is null)
        {
            UnsubscribeRendering();
        }
    }

    private void ApplyStripVisuals()
    {
        var reveal = Math.Clamp(_panelReveal.Value, 0, 1);
        var eased = reveal * reveal * (3 - 2 * reveal);
        StripTranslate.X = -(1 - eased) * Math.Max(80, ActualWidth * 0.7);
        StripRoot.Opacity = eased;

        var scrollOffset = -_scroll.Value * _slotExtent;
        var lastSlot = _visibleSlots - 1;
        foreach (var card in AllCards)
        {
            var slot = (card.CenterY.Value + scrollOffset - _viewportTop) / _slotExtent - 0.5;
            var outside = Math.Max(0, Math.Max(-slot, slot - lastSlot));
            card.ApplyVisuals(Math.Clamp(1 - outside * 1.6, 0, 1), scrollOffset, _stripScale.Value);
        }

        UpdateVisualLayers();
    }

    private void UpdateVisualLayers()
    {
        if (_windowHandle == 0 ||
            !IsVisible ||
            PresentationSource.FromVisual(this)?.CompositionTarget is not { } compositionTarget)
        {
            return;
        }

        var toDevice = compositionTarget.TransformToDevice;
        var clip = new NativeMethods.Rect
        {
            Right = (int)Math.Ceiling(ActualWidth * toDevice.M11),
            Bottom = (int)Math.Ceiling(ActualHeight * toDevice.M22)
        };
        var stripOpacity = StripRoot.Opacity;
        var badges = new List<IconOverlayItem>();
        foreach (var card in AllCards)
        {
            card.UpdateThumbnails(this, toDevice, clip, stripOpacity, StripHeight / 2);
            if (card.GetBadge(this, stripOpacity) is { } badge)
            {
                badges.Add(badge);
            }
        }

        if (_iconOverlay is { IsVisible: true })
        {
            _iconOverlay.SyncToOwner(this);
            _iconOverlay.UpdateBadges(badges);
        }
    }

    /// <summary>
    /// The strip's centre line in screen pixels. Turned cards lean toward it,
    /// and flights use it so a window is drawn the same way in the air and in the strip.
    /// </summary>
    private float GetVanishingScreenY() =>
        PresentationSource.FromVisual(this)?.CompositionTarget is { } compositionTarget &&
        NativeMethods.GetWindowRect(_windowHandle, out var window)
            ? (float)(window.Top + StripHeight / 2 * compositionTarget.TransformToDevice.M22)
            : 0;

    /// <summary>The shadow Windows draws around a window on the stage, in screen pixels.</summary>
    private ShadowStyle GetWindowShadow(bool isActive) => ShadowStyle.Window(GetDeviceScale(), isActive);

    /// <summary>The shadow a settled card draws under each of its windows, in screen pixels.</summary>
    private ShadowStyle GetCardShadow() => WindowPreviewControl.GetShadowStyle(GetDeviceScale(), _stripScale.Target);

    // The stage is the strip's own monitor, so both kinds of shadow share its scale.
    private double GetDeviceScale() =>
        PresentationSource.FromVisual(this)?.CompositionTarget?.TransformToDevice.M11 ?? 1;

    /// <summary>Where a card currently draws one of its windows, in screen pixels.</summary>
    private RectangleF? GetCardScreenRect(WindowPreviewControl card, nint window)
    {
        if (PresentationSource.FromVisual(this)?.CompositionTarget is not { } compositionTarget ||
            !NativeMethods.GetWindowRect(_windowHandle, out var origin) ||
            card.GetCurrentDeviceBounds(this, compositionTarget.TransformToDevice, window) is not { } bounds)
        {
            return null;
        }

        return new RectangleF(
            (float)(origin.Left + bounds.Left),
            (float)(origin.Top + bounds.Top),
            (float)bounds.Width,
            (float)bounds.Height);
    }

    /// <summary>
    /// Where a card will draw a window once the strip has settled: fully
    /// revealed, scrolled into place and at rest with its current hover.
    /// </summary>
    private RectangleF? GetCardTargetScreenRect(WindowPreviewControl card, nint window)
    {
        if (PresentationSource.FromVisual(this)?.CompositionTarget is not { } compositionTarget ||
            !NativeMethods.GetWindowRect(_windowHandle, out var origin) ||
            card.GetRestingLayerBounds(
                window,
                card.CenterY.Target - _scroll.Target * _slotExtent,
                _stripScale.Target) is not { } local)
        {
            return null;
        }

        var bounds = Rect.Transform(local, compositionTarget.TransformToDevice);
        return new RectangleF(
            (float)(origin.Left + bounds.Left),
            (float)(origin.Top + bounds.Top),
            (float)bounds.Width,
            (float)bounds.Height);
    }

    /// <summary>
    /// Settles a card's hover on whether the pointer will be on it once it rests in
    /// its slot, before a window flies into it. The flight then lands in the pose
    /// the card shows, hovered or turned away, instead of the card switching to
    /// hovered the moment it takes over from the flight.
    /// </summary>
    private void SettleHover(WindowPreviewControl card)
    {
        var isPointerOn = false;
        if (PresentationSource.FromVisual(StripCanvas) is not null)
        {
            var cursor = System.Windows.Forms.Cursor.Position;
            isPointerOn = card.ContainsAtRest(
                StripCanvas.PointFromScreen(new System.Windows.Point(cursor.X, cursor.Y)),
                card.CenterY.Target - _scroll.Target * _slotExtent,
                _stripScale.Target);
        }

        card.Hover.Snap(isPointerOn ? 1 : 0);
    }

    /// <summary>The part of the strip window that cards occupy, in screen pixels.</summary>
    private System.Drawing.Rectangle GetCardsScreenBounds()
    {
        if (_cards.Count == 0 ||
            PresentationSource.FromVisual(this)?.CompositionTarget is not { } compositionTarget ||
            !NativeMethods.GetWindowRect(_windowHandle, out var window))
        {
            return System.Drawing.Rectangle.Empty;
        }

        var scaleY = compositionTarget.TransformToDevice.M22;
        var top = window.Top + (int)Math.Floor(_viewportTop * scaleY);
        var bottom = window.Top + (int)Math.Ceiling((_viewportTop + _visibleSlots * _slotExtent) * scaleY);
        return System.Drawing.Rectangle.FromLTRB(window.Left, top, window.Right, bottom);
    }

    private bool IsCardOnScreen(WindowPreviewControl card) =>
        IsVisible && _panelReveal.Value > 0.9 && card.ViewportFactor > 0.5 && !card.IsInFlight;
}
