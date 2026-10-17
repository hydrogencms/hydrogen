export default function NotFound() {
  return (
    <section className="container notfound" data-template="not-found">
      <p className="code">404</p>
      <h1>Page not found</h1>
      <p>The page you are looking for does not exist or has moved.</p>
      <a className="button" href="/">Back to the home page</a>
    </section>
  )
}
