import { useState, useEffect } from 'react'

function App() {
  const [rules, setRules] = useState([])
  const [loading, setLoading] = useState(true)
  const [error, setError] = useState(null)

  useEffect(() => {
    fetchRules()
  }, [])

  const fetchRules = async () => {
    try {
      const response = await fetch('/api/v1/rules')
      if (!response.ok) throw new Error('Failed to fetch rules')
      const data = await response.json()
      setRules(data || [])
    } catch (err) {
      setError(err.message)
    } finally {
      setLoading(false)
    }
  }

  return (
    <div style={{ padding: '20px', fontFamily: 'sans-serif' }}>
      <h1>DSProxy Rules</h1>
      {loading && <p>Loading...</p>}
      {error && <p style={{ color: 'red' }}>Error: {error}</p>}
      
      <table border="1" cellPadding="10" style={{ borderCollapse: 'collapse', width: '100%' }}>
        <thead>
          <tr>
            <th>Name</th>
            <th>User/Group</th>
            <th>DataSource</th>
            <th>Permissions</th>
          </tr>
        </thead>
        <tbody>
          {rules.map(rule => (
            <tr key={rule.metadata.name}>
              <td>{rule.metadata.name}</td>
              <td>{rule.spec.user || rule.spec.group}</td>
              <td>{rule.spec.dataSourceId}</td>
              <td>
                {rule.spec.permissions.map((p, i) => (
                  <div key={i}>{p.action} on {p.resource}</div>
                ))}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  )
}

export default App
